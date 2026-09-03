package retrieve

import (
	"context"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/agent/guardian"
	"github.com/WWaynee/content-hub/api/service"
)

// claimLoopThinker 给单个未覆盖 claim 换 query 的默认思路（P06）：
// 逐轮在检索词上补充更宽松/替代措辞以尝试不同召回；若 n 轮仍不足则放弃该角度（交给 Guardian 判 ask_human）。
// 说明：真正"语义换问法"由上层在 P06 之后的接入里可用 LLM 注入更强的 Thinker；此处先保证
// "多轮会发起 >1 次检索、也不超 budget、也不自编"，是 Guardian 循环能站立的最小产线默认。
const (
	defaultBudget = 3
)

type stagedThinker struct {
	round int
}

func (t *stagedThinker) call(ctx context.Context, claim string, tried []string) (string, bool, error) {
	switch t.round {
	case 0:
		t.round++
		return claim, true, nil
	case 1:
		t.round++
		return "、" + claim, true, nil // 尝试换一种更宽的表述再搜一次（仍以原文为准，不自编）
	case 2:
		t.round++
		return "规定 要求 " + claim, true, nil
	default:
		return "", false, nil
	}
}// LoopSearchClaim 对单个 claim(文本)执行「可多次换 query 的检索」，返回 Guardian 三态裁决。
// 这是让检索"覆盖不足会自动换角度、仍不足给缺证而非硬抛"的产品级默认接线（对单点）。
// ScopeIDs 空=全租户；budget<=0 用默认。
func LoopSearchClaim(ctx context.Context, tenantID uint64, fileIDs []uint64, claim string, budget int) (*guardian.Decision, error) {
	if budget <= 0 {
		budget = defaultBudget
	}
	sr := service.SearchKbaseSentences // 注入为检索端子
	return guardian.Judge(ctx, guardian.Options{
		TenantID: tenantID, ScopeIDs: fileIDs,
		Budget: budget, MinCover: 1, Claim: claim,
		Search: func(ctx2 context.Context, tenant uint64, query string, fids ...uint64) ([]agent.Evidence, error) {
			hits, err := sr(ctx2, tenant, query, fids...)
			if err != nil {
				return nil, err
			}
			return hitsToEvidence2(hits), nil
		},
		Think: (&stagedThinker{}).call,
	})
}

// hitsToEvidence2 复用既有 KbaseHit→agent.Evidence 转换（同 service 内 hitsToEvidence 语义，retrieve 站内避免跨表）。
func hitsToEvidence2(hits []service.KbaseHit) []agent.Evidence {
	out := make([]agent.Evidence, 0, len(hits))
	for _, h := range hits {
		out = append(out, agent.Evidence{
			FileID: h.FileID, DocSentenceID: h.DocSentenceID, ChunkID: h.ChunkID,
			VersionMd5: h.VersionMd5, ChapterTitle: h.ChapterTitle,
			SourceText: h.SourceText, Score: h.Score,
		})
	}
	return out
}

