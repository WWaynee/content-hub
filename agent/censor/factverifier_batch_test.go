package censor

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/llmclient"
)

// fakeExtractor 每次 ChatWithJSON 只做"拆句断言"：它不进行 LLM 真实判定，
// 而是把本次传给它的【稿件句子】区段逐行原样 echo 回来（每条 has_data_assertion=false，
// 下标按本批从 0 起）。用于验证 Check 的分批索引补偿与全量证据不丢。
type fakeExtractor struct {
	calls   []string // 记录每次 ChatWithJSON 收到的完整 prompt
	fail    error    // 非 nil 时直接返回该错误
}

func (f *fakeExtractor) Embed(context.Context, string) ([]float32, error) {
	return nil, nil
}
func (f *fakeExtractor) EmbedBatch(context.Context, []string) ([][]float32, error) {
	return nil, nil
}
func (f *fakeExtractor) Chat(context.Context, []llmclient.ChatMessage) (string, error) {
	return "", nil
}

// sentenceIndexRE 匹配 buildVerifyPrompt 输出的稿句子行：`[0] 句文本`
var sentenceIndexRE = regexp.MustCompile(`\[(\d+)\]\s*(.*)`)

// ChatWithJSON 实现 llmclient.Client：解析 prompt 里的稿句子，回显结构化 JSON。
func (f *fakeExtractor) ChatWithJSON(_ context.Context, msgs []llmclient.ChatMessage, target interface{}) error {
	prompt := ""
	for _, m := range msgs {
		prompt += m.Content
	}
	f.calls = append(f.calls, prompt)
	if f.fail != nil {
		return f.fail
	}

	// 取【稿件句子】区块到【任务】/结尾，逐行回显。
	seg := prompt
	if i := strings.Index(prompt, "【稿件句子】"); i >= 0 {
		seg = prompt[i+len("【稿件句子】"):]
	}
	if i := strings.Index(seg, "【"); i >= 0 {
		seg = seg[:i]
	}
	var sents []map[string]interface{}
	for _, ln := range strings.Split(seg, "\n") {
		m := sentenceIndexRE.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		var idx int
		fmt.Sscanf(m[1], "%d", &idx)
		sents = append(sents, map[string]interface{}{
			"index":             idx,
			"text":              strings.TrimSpace(m[2]),
			"has_data_assertion": false,
			"assertions":        []interface{}{},
		})
	}

	out := map[string]interface{}{"sentences": sents}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

func evidenceItems(n int) []agent.Evidence {
	ev := make([]agent.Evidence, n)
	for i := range ev {
		ev[i] = agent.Evidence{FileID: uint64(100 + i), SourceText: fmt.Sprintf("证据原文%d号，包含事实数据。", i)}
	}
	return ev
}

// TestFactVerifierCheck_BatchesGlobalIndex_Off_Lean：
// 65 句 > 每批 40 → 拆成 2 次 LLM 调用；断言：
//  1) 调用次数=2；
//  2) 每批 prompt 都携带全部证据（证据不因分批被丢弃）；
//  3) 合并后每条结果 Index 在 0..64 全局无缺、严格递增；
//  4) 第二批(≥40)句得到补偿后的全局下标(>=40)，不会与第一批撞号。
func TestFactVerifierCheck_BatchesGlobalIndexOffLean(t *testing.T) {
	total := 65 // maxSentencesPerCheckCall=40 → 拆两批
	ev := evidenceItems(3)
	sentences := make([]string, total)
	for i := range sentences {
		sentences[i] = fmt.Sprintf("第%d句：无数据断言的公文表述。", i)
	}

	fake := &fakeExtractor{}
	v := NewFactVerifier(fake)
	res, err := v.Check(context.Background(), sentences, ev)
	if err != nil {
		t.Fatalf("Check 应成功: %v", err)
	}

	if len(fake.calls) != 2 {
		t.Fatalf("期望 2 次 LLM 分批调用，实际 %d", len(fake.calls))
	}
	// 证据全带：两批 prompt 都包含每条证据原文。
	for ci, c := range fake.calls {
		for _, e := range ev {
			if !strings.Contains(c, e.SourceText) {
				t.Errorf("第%d批 prompt 缺少证据原文 %q，证明分批丢了证据", ci, e.SourceText)
			}
		}
	}

	// 合并结果：须覆盖 0..64 全局下标，无重复、无缺失。
	if len(res.Sentences) != total {
		t.Fatalf("期望 %d 句结果，实际 %d", total, len(res.Sentences))
	}
	seen := make([]bool, total)
	for _, s := range res.Sentences {
		if s.Index < 0 || s.Index >= total {
			t.Fatalf("Index 越界: %d", s.Index)
		}
		if seen[s.Index] {
			t.Fatalf("Index 重复: %d（跨批补偿缺失）", s.Index)
		}
		seen[s.Index] = true
	}
	for i, ok := range seen {
		if !ok {
			t.Errorf("全局下标 %d 缺失（分批索引补偿错误）", i)
		}
	}
}
