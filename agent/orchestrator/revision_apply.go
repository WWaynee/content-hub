package orchestrator

import "github.com/WWaynee/content-hub/agent"

// ApplySentenceRevision 把一次句子修订应用到稿件句子列表，体现「未动句继承」。
//   - sentences：修订前的稿件句子（含每句的证据索引）
//   - rev：被改句的修订结果（新文本 + 新证据索引）
// 返回新的句子列表：被改句更新为（新文本, 新证据），其余句子原样保留（继承原证据）。
func ApplySentenceRevision(sentences []agent.Sentence, targetIndex int, newText string, newEvidenceRefs []uint64) []agent.Sentence {
	out := make([]agent.Sentence, len(sentences))
	copy(out, sentences)
	if targetIndex >= 0 && targetIndex < len(out) {
		out[targetIndex] = agent.Sentence{
			Text:         newText,
			EvidenceRefs: newEvidenceRefs,
		}
	}
	return out
}
