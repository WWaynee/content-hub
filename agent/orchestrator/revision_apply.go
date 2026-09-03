// DEPRECATED(P07 / rev-3 C5)：ApplySentenceRevision 只是带"未动句继承"的纯函数对照实现；
// 运行期修订统一由 api/service 的 run 链(ReviseSentenceFull→ApplyArticleRevision→P08 change_list)承担。
// 本文件保留仅作局部单测参照,勿再由此引出第二套修订写库路径。
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
