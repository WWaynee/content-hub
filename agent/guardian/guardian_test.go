package guardian

import (
	"context"
	"testing"

	"github.com/WWaynee/content-hub/agent"
)

// seqThink 顺序返回候选 query；用完返回想不出(false)。
type seqThink struct {
	qs []string
	i  int
}

func (t *seqThink) call(ctx context.Context, claim string, tried []string) (string, bool, error) {
	for t.i < len(t.qs) {
		q := t.qs[t.i]
		t.i++
		return q, true, nil
	}
	return "", false, nil
}

func countSearch(out map[string][]agent.Evidence, n *int) SearchFn {
	return func(ctx context.Context, tenantID uint64, query string, fileIDs ...uint64) ([]agent.Evidence, error) {
		*n++
		return out[query], nil
	}
}

// 1) 首轮单 query 召回不足、需二次换 query 才能覆盖 → 断言会发起 >1 检索且不超 budget；结果 accept。
func TestGuardian_RetrieveMoreThanOnceUntilCovered(t *testing.T) {
	s0 := &seqThink{qs: []string{"A", "B"}}
	calls := 0
	search := countSearch(map[string][]agent.Evidence{
		"A": nil,
		"B": {{DocSentenceID: 11, SourceText: "来源含数据", FileID: 2}},
	}, &calls)

	dec, err := Judge(context.Background(), Options{
		Budget: 5, MinCover: 1, Claim: "今年场次",
		Search: search, Think: s0.call,
	})
	if err != nil {
		t.Fatalf("Judge 失败: %v", err)
	}
	if dec.Verdict != VerdictAccept {
		t.Fatalf("应 accept，实得 %s(%s)", dec.Verdict, dec.Reason)
	}
	if calls < 2 {
		t.Fatalf("应发起 >1 次检索(换 query 才覆盖)，实得 %d 次", calls)
	}
	if calls > 5 {
		t.Fatalf("不应超 budget(5)，实得 %d", calls)
	}
	if len(dec.Tried) != calls {
		t.Errorf("Tried 长度应等于实检索次数 %d，实得 %d", calls, len(dec.Tried))
	}
}

// 2) 无论怎么查都无源 → ask_human 而非返回 Err ... 一类的硬抛。
func TestGuardian_NoSourceGoesAskHuman(t *testing.T) {
	s0 := &seqThink{qs: []string{"桩", "椅", "塔"}}
	calls := 0
	search := countSearch(map[string][]agent.Evidence{}, &calls) // 永远空

	dec, err := Judge(context.Background(), Options{
		Budget: 3, MinCover: 1, Claim: "某阈值",
		Search: search, Think: s0.call,
	})
	if err != nil {
		t.Fatalf("无源也不应抛错误(guardian 应转 ask_human)，实得 %v", err)
	}
	if dec.Verdict != VerdictAskHuman {
		t.Fatalf("应 ask_human，实得 %s", dec.Verdict)
	}
	if calls > 3 {
		t.Fatalf("检索次数不应超 budget(3)，实得 %d", calls)
	}
	if len(dec.Missing) == 0 {
		t.Error("ask_human 应带缺证描述")
	}
}

// 3) Think 一直给同一 query（不会换新角度）→ 防重复不空转，次数受 budget 约束。
func TestGuardian_DedupNoEndless(t *testing.T) {
	think := func(ctx context.Context, claim string, tried []string) (string, bool, error) {
		return "同一条", true, nil
	}
	calls := 0
	search := countSearch(map[string][]agent.Evidence{}, &calls)

	dec, err := Judge(context.Background(), Options{
		Budget: 3, MinCover: 1, Claim: "c",
		Search: search, Think: think,
	})
	if err != nil {
		t.Fatalf("不应抛错(judge 内部去重/预算护栏): %v", err)
	}
	// 重复 query 不应导致搜索无限：真实检索 <= 首次 1 次 + 无新角度即停
	if calls > 3 {
		t.Fatalf("同 query 重复不应空转，calls=%d 超过 budget", calls)
	}
	_ = dec
}
