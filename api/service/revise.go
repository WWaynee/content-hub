package service

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/agent"
	agentcensor "github.com/WWaynee/content-hub/agent/censor"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 段落/句子级 AI 修订的落库（未动句继承 + 被改句更新 + 落新快照）。

// ReviseSentenceInput 一次句子修订的输入。
type ReviseSentenceInput struct {
	WorkspaceID     uint64
	TenantID        uint64
	TargetIndex     int              // 被改句序号（在整篇稿件的句子序列中的下标）
	NewText         string           // 新句子文本
	NewEvidence     []agent.Evidence // 被改句重检测到的证据
	NewEvidenceRefs []uint64         // 被改句新绑定的证据索引（指向 NewEvidence）
}

// sentDraft 句子草稿，记录其原句 index（用于绑定关联）。
type sentDraft struct {
	content string
	// 未动句继承的原 doc_sentence 绑定；被改句为空（用 NewEvidence 重建）
	inheritedBinds []model.EvidenceBinding
}

// ApplyArticleRevision 把一次句子修订应用并落新稿件快照：未动句继承原文本+原证据，被改句更新。
// 返回新 article_version ID。
func ApplyArticleRevision(ctx context.Context, in ReviseSentenceInput) (uint64, error) {
	a, err := storage.GetArticleByWorkspace(ctx, in.TenantID, in.WorkspaceID)
	if err != nil {
		return 0, fmt.Errorf("稿件不存在")
	}
	prev, err := storage.GetLatestArticleVersion(ctx, a.ID)
	if err != nil {
		return 0, fmt.Errorf("稿件版本不存在")
	}
	sents, err := storage.ListArticleSentences(ctx, prev.ID)
	if err != nil {
		return 0, err
	}
	binds, err := storage.ListArticleBindings(ctx, prev.ID)
	if err != nil {
		return 0, err
	}
	// 乐观锁：原子地把 current_version_no: baseVer → nextNo。只有唯一成功者继续写快照；
	// 并发另一方 CAS 失败即返回冲突，不再产生重复 version_no。
	baseVer := uint64(a.CurrentVersionNo)
	nextNo := baseVer + 1
	if ok, cerr := storage.CASBumpArticleCurrentVersion(ctx, a.ID, baseVer, nextNo); cerr != nil {
		return 0, fmt.Errorf("推进稿件版本失败: %w", cerr)
	} else if !ok {
		return 0, ErrArticleVersionConflict
	}

	if in.TargetIndex < 0 || in.TargetIndex >= len(sents) {
		return 0, fmt.Errorf("目标句子序号越界: %d", in.TargetIndex)
	}

	// 按 sentence_id 分组原绑定
	bindBySent := map[uint64][]model.EvidenceBinding{}
	for _, b := range binds {
		bindBySent[b.ArticleSentenceID] = append(bindBySent[b.ArticleSentenceID], b)
	}

	// 组装新句子草稿：未动句继承内容+绑定，被改句换内容
	drafts := make([]sentDraft, len(sents))
	for i, s := range sents {
		if i == in.TargetIndex {
			drafts[i] = sentDraft{content: in.NewText}
		} else {
			// 继承内容 + 该句原绑定（ID 清零，落新库）
			inherited := bindBySent[s.ID]
			for j := range inherited {
				inherited[j].ID = 0
				inherited[j].ArticleVersionID = 0
				inherited[j].ArticleSentenceID = 0 // 落库时回填新句 ID
			}
			drafts[i] = sentDraft{content: s.Content, inheritedBinds: inherited}
		}
	}

	newVer := &model.ArticleVersion{
		ArticleID:         a.ID,
		WorkspaceID:       in.WorkspaceID,
		TenantID:          in.TenantID,
		VersionNo:         int(nextNo),
		FullContent:       buildContentFromDrafts(drafts),
		Status:            "completed",
		ReferencedVersion: int(baseVer),
	}

	// 事务落库：version → sentences（拿新 ID）→ bindings（按 index 关联 + 被改句新证据）
	err = storage.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(newVer).Error; err != nil {
			return err
		}
		newSents := make([]model.ArticleSentence, len(drafts))
		for i, d := range drafts {
			newSents[i] = model.ArticleSentence{
				ArticleVersionID: newVer.ID,
				WorkspaceID:      in.WorkspaceID,
				TenantID:         in.TenantID,
				SentenceIndex:    i,
				Content:          d.content,
			}
		}
		if err := tx.Create(&newSents).Error; err != nil {
			return err
		}

		// bindings
		for i, d := range drafts {
			if i == in.TargetIndex {
				// 被改句：新证据
				for orderNo, ref := range in.NewEvidenceRefs {
					if int(ref) >= len(in.NewEvidence) {
						continue
					}
					e := in.NewEvidence[ref]
					b := model.EvidenceBinding{
						ArticleVersionID:  newVer.ID,
						ArticleSentenceID: newSents[i].ID,
						TenantID:          in.TenantID,
						SourceType:        "knowledge",
						DocFileID:         e.FileID,
						DocSentenceID:     e.DocSentenceID,
						EvidenceStatus:    "bound",
						OrderNo:           orderNo,
					}
					if err := tx.Create(&b).Error; err != nil {
						return err
					}
				}
			} else {
				// 未动句：继承原绑定（回填新句 ID）
				for _, ib := range d.inheritedBinds {
					ib.ArticleVersionID = newVer.ID
					ib.ArticleSentenceID = newSents[i].ID
					if err := tx.Create(&ib).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("落修订快照失败: %w", err)
	}
	return newVer.ID, nil
}

func buildContentFromDrafts(drafts []sentDraft) string {
	var sb strings.Builder
	for _, d := range drafts {
		sb.WriteString(d.content)
	}
	return sb.String()
}

// ReviseSentenceFull 句子级修订的完整链路：读当前稿件 → LLM 重写目标句 → 被改句重检测证据 → 落新快照。
// 这是 revision 的"对话→重写→落库"最后一环。
func ReviseSentenceFull(ctx context.Context, tenantID, workspaceID uint64, targetIndex int, instruction string) (uint64, error) {
	// 0. 惰性失效判定：需求单变更后禁止基于过期检索快照做局部修订
	req, err := storage.GetRequirementByWorkspace(ctx, tenantID, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("需求单不存在")
	}
	if err := EnsureBatchFresh(ctx, workspaceID, req.ID); err != nil {
		return 0, err
	}

	// 1. 读当前稿件句子
	a, err := storage.GetArticleByWorkspace(ctx, tenantID, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("稿件不存在")
	}
	prev, err := storage.GetLatestArticleVersion(ctx, a.ID)
	if err != nil {
		return 0, fmt.Errorf("稿件版本不存在")
	}
	sents, err := storage.ListArticleSentences(ctx, prev.ID)
	if err != nil {
		return 0, err
	}
	if targetIndex < 0 || targetIndex >= len(sents) {
		return 0, fmt.Errorf("目标句子序号越界: %d", targetIndex)
	}
	targetText := sents[targetIndex].Content

	// 2. 勾选范围
	fileIDs, err := RequirementFileIDScope(ctx, tenantID, req.ID)
	if err != nil {
		return 0, err
	}

	// 3. LLM 重写目标句（带原句 + 上下文）
	newText, err := rewriteSentenceLLM(ctx, targetText, instruction)
	if err != nil {
		return 0, err
	}

	// 4. 被改句重检测证据（方案甲：对新句内容在勾选范围内重新检索）
	hits, err := SearchKbaseSentences(ctx, tenantID, newText, fileIDs...)
	if err != nil {
		return 0, err
	}
	newEvidence := hitsToEvidence(hits)

	// 闸门三：修订内容若含数据断言但无证据支撑 → 拒绝写入
	if ok, rerr := checkRevisionFactSupport(ctx, newText, newEvidence); !ok {
		return 0, rerr
	}

	// 落本次修订的检索快照（惰性失效判定基准）
	if _, berr := PersistRetrievalBatch(ctx, tenantID, workspaceID, req.ID, req.Version, []string{newText}, hits); berr != nil {
		_ = berr
	}

	// 5. 落新快照（未动句继承，被改句换新文本 + 新证据）
	// 新证据的 refs：取前几条作为绑定（一期简化：全部命中都可绑，但限制最多前 3 条）
	refs := make([]uint64, 0, len(newEvidence))
	for i := range newEvidence {
		if i >= 3 {
			break
		}
		refs = append(refs, uint64(i))
	}
	return ApplyArticleRevision(ctx, ReviseSentenceInput{
		WorkspaceID:     workspaceID,
		TenantID:        tenantID,
		TargetIndex:     targetIndex,
		NewText:         newText,
		NewEvidence:     newEvidence,
		NewEvidenceRefs: refs,
	})
}

// rewriteSentenceLLM 调 LLM 重写单个句子（带原句 + 修改要求）。
func rewriteSentenceLLM(ctx context.Context, originalText, instruction string) (string, error) {
	llm := llmclient.NewClient()
	prompt := fmt.Sprintf("请重写下面这句话，满足修改要求。\n\n原句：%s\n修改要求：%s\n\n只返回 JSON：{\"text\":\"重写后的句子\"}", originalText, instruction)
	var out struct {
		Text string `json:"text"`
	}
	if err := llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: prompt}}, &out); err != nil {
		return "", fmt.Errorf("句子重写失败: %w", err)
	}
	if out.Text == "" {
		return "", fmt.Errorf("LLM 未返回重写句子")
	}
	return out.Text, nil
}

// hitsToEvidence 把句子级 KbaseHit 转成 agent.Evidence。
func hitsToEvidence(hits []KbaseHit) []agent.Evidence {
	out := make([]agent.Evidence, 0, len(hits))
	for _, h := range hits {
		out = append(out, agent.Evidence{
			FileID:        h.FileID,
			DocSentenceID: h.DocSentenceID,
			ChunkID:       h.ChunkID,
			VersionMd5:    h.VersionMd5,
			ChapterTitle:  h.ChapterTitle,
			SourceText:    h.SourceText,
			Score:         h.Score,
		})
	}
	return out
}

// ErrRevisionNoSupport 修订/追加内容无法在知识库中找到支撑证据。
type ErrRevisionNoSupport struct {
	Reason string
}

func (e *ErrRevisionNoSupport) Error() string { return e.Reason }

// checkRevisionFactSupport 对修订/追加的新文本做「事实断言 + 证据支撑」校验（闸门三）。
// 规则（对齐能力边界）：
//   - 新文本含数据/事实断言但无法在证据原文中找到直接支撑 → 返回 (false, ok)，调用方应拒绝写入。
//   - 新文本不含数据断言（纯公文措辞）或断言都有证据支撑 → 返回 (true, ok) 放行。
//   - 无法解析（LLM 校验失败）时按「无数据断言」放行，避免误伤纯措辞修订。
func checkRevisionFactSupport(ctx context.Context, newText string, newEvidence []agent.Evidence) (bool, error) {
	if newText == "" {
		return true, nil
	}
	verifier := agentcensor.NewFactVerifier(llmclient.NewClient())
	fc, err := verifier.Check(ctx, []string{newText}, newEvidence)
	if err != nil {
		// 校验 LLM 失败不阻断——按纯措辞放行，主流程优先
		return true, nil
	}
	for _, s := range fc.Sentences {
		if !s.HasDataAssertion {
			continue
		}
		for _, a := range s.Assertions {
			if !a.Supported {
				return false, &ErrRevisionNoSupport{
					Reason: fmt.Sprintf("本条修改含如下数据，但知识库中找不到支撑证据：%s", a.Text),
				}
			}
		}
	}
	return true, nil
}

// AppendArticleContent 追加段落：LLM 生成追加内容 → 检索证据 → 追加到稿件末尾 → 落新快照。
// 现有句子全部继承（文本+证据），追加的新句子带新检索到的证据。
func AppendArticleContent(ctx context.Context, tenantID, workspaceID uint64, instruction string) (uint64, error) {
	// 0. 惰性失效判定：需求单变更后禁止基于过期检索快照做追加
	req, err := storage.GetRequirementByWorkspace(ctx, tenantID, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("需求单不存在")
	}
	if err := EnsureBatchFresh(ctx, workspaceID, req.ID); err != nil {
		return 0, err
	}

	a, err := storage.GetArticleByWorkspace(ctx, tenantID, workspaceID)
	if err != nil {
		return 0, fmt.Errorf("稿件不存在")
	}
	prev, err := storage.GetLatestArticleVersion(ctx, a.ID)
	if err != nil {
		return 0, fmt.Errorf("稿件版本不存在")
	}
	sents, err := storage.ListArticleSentences(ctx, prev.ID)
	if err != nil {
		return 0, err
	}
	binds, err := storage.ListArticleBindings(ctx, prev.ID)
	if err != nil {
		return 0, err
	}

	// LLM 生成追加内容（一句话或几句话）
	newText, err := appendContentLLM(ctx, instruction)
	if err != nil {
		return 0, err
	}

	// 对追加内容检索证据
	fileIDs, err := RequirementFileIDScope(ctx, tenantID, req.ID)
	if err != nil {
		return 0, err
	}
	hits, err := SearchKbaseSentences(ctx, tenantID, newText, fileIDs...)
	if err != nil {
		return 0, err
	}
	newEvidence := hitsToEvidence(hits)

	// 闸门三：追加内容若含数据断言但无证据支撑 → 拒绝写入
	if ok, rerr := checkRevisionFactSupport(ctx, newText, newEvidence); !ok {
		return 0, rerr
	}

	// 落本次追加的检索快照（惰性失效判定基准）
	if _, berr := PersistRetrievalBatch(ctx, tenantID, workspaceID, req.ID, req.Version, []string{newText}, hits); berr != nil {
		_ = berr
	}

	// 乐观锁：原子地把 current_version_no: baseVer → nextNo；并发冲突返回，不产生重复 version_no。
	baseVer := uint64(a.CurrentVersionNo)
	nextNo := baseVer + 1
	if ok, cerr := storage.CASBumpArticleCurrentVersion(ctx, a.ID, baseVer, nextNo); cerr != nil {
		return 0, fmt.Errorf("推进稿件版本失败: %w", cerr)
	} else if !ok {
		return 0, ErrArticleVersionConflict
	}

	// 现有句子绑定按 sentence_id 分组（继承）
	bindBySent := map[uint64][]model.EvidenceBinding{}
	for _, b := range binds {
		bindBySent[b.ArticleSentenceID] = append(bindBySent[b.ArticleSentenceID], b)
	}

	// 组装完整新句子列表：现有句子（继承绑定）+ 追加句（新证据）
	type draft struct {
		content string
		binds   []model.EvidenceBinding
	}
	oldSentenceID := make([]uint64, len(sents))
	drafts := make([]draft, 0, len(sents)+1)
	for i, s := range sents {
		oldSentenceID[i] = s.ID
		inherited := bindBySent[s.ID]
		for j := range inherited {
			inherited[j].ID = 0
			inherited[j].ArticleVersionID = 0
			inherited[j].ArticleSentenceID = 0
		}
		drafts = append(drafts, draft{content: s.Content, binds: inherited})
	}
	// 追加句的绑定：取前 3 条新证据
	appendBinds := []model.EvidenceBinding{}
	for i, e := range newEvidence {
		if i >= 3 {
			break
		}
		appendBinds = append(appendBinds, model.EvidenceBinding{
			TenantID:       tenantID,
			SourceType:     "knowledge",
			DocFileID:      e.FileID,
			DocSentenceID:  e.DocSentenceID,
			EvidenceStatus: "bound",
			OrderNo:        i,
		})
	}
	drafts = append(drafts, draft{content: newText, binds: appendBinds})

	newVer := &model.ArticleVersion{
		ArticleID:         a.ID,
		WorkspaceID:       workspaceID,
		TenantID:          tenantID,
		VersionNo:         int(nextNo),
		FullContent:       buildAppendedContent(sents, newText),
		Status:            "completed",
		ReferencedVersion: int(baseVer),
	}

	err = storage.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(newVer).Error; err != nil {
			return err
		}
		newSents := make([]model.ArticleSentence, len(drafts))
		for i, d := range drafts {
			newSents[i] = model.ArticleSentence{
				ArticleVersionID: newVer.ID,
				WorkspaceID:      workspaceID,
				TenantID:         tenantID,
				SentenceIndex:    i,
				Content:          d.content,
			}
		}
		if err := tx.Create(&newSents).Error; err != nil {
			return err
		}
		for i, d := range drafts {
			for _, b := range d.binds {
				b.ArticleVersionID = newVer.ID
				b.ArticleSentenceID = newSents[i].ID
				if err := tx.Create(&b).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("落追加快照失败: %w", err)
	}
	return newVer.ID, nil
}

// appendContentLLM 调 LLM 生成一段追加内容。
func appendContentLLM(ctx context.Context, instruction string) (string, error) {
	llm := llmclient.NewClient()
	prompt := fmt.Sprintf("请根据下面的要求，为稿件追加一段内容（一段话，可含多个句子）。\n\n要求：%s\n\n只返回 JSON：{\"text\":\"追加的段落内容\"}", instruction)
	var out struct {
		Text string `json:"text"`
	}
	if err := llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: prompt}}, &out); err != nil {
		return "", fmt.Errorf("追加内容生成失败: %w", err)
	}
	if out.Text == "" {
		return "", fmt.Errorf("LLM 未返回追加内容")
	}
	return out.Text, nil
}

func buildAppendedContent(sents []model.ArticleSentence, appended string) string {
	var sb strings.Builder
	for _, s := range sents {
		sb.WriteString(s.Content)
	}
	sb.WriteString(appended)
	return sb.String()
}
