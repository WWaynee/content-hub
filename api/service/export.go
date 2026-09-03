package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 导出：合并 md（正文 + 证据清单）。

// ExportArticle 导出某稿件版本为合并 md（正文在前，证据清单在后）。
// 导出状态机：稿件处于 generating/revising（修改中）时禁止导出。
func ExportArticle(ctx context.Context, articleVersionID uint64) (string, error) {
	var ver model.ArticleVersion
	if err := storage.GetDB().WithContext(ctx).Where("id = ?", articleVersionID).First(&ver).Error; err != nil {
		return "", fmt.Errorf("查稿件版本失败: %w", err)
	}

	// 状态机：修改中禁导
	var ws model.Workspace
	if err := storage.GetDB().WithContext(ctx).Where("id = ?", ver.WorkspaceID).First(&ws).Error; err == nil {
		if ws.Status == "generating" || ws.Status == "revising" {
			return "", fmt.Errorf("稿件正在生成/修订中，暂不可导出")
		}
	}

	// 1. 该版本的句子（按版本内插入序 = 正文自然序列）
	var sents []model.ArticleSentence
	storage.GetDB().WithContext(ctx).Where("article_version_id = ?", articleVersionID).
		Order("id ASC").Find(&sents)

	// 2. 证据绑定（按 sentence + order_no）
	var binds []model.EvidenceBinding
	storage.GetDB().WithContext(ctx).Where("article_version_id = ?", articleVersionID).
		Order("article_sentence_id ASC, order_no ASC").Find(&binds)

	// 3. 组装 md：正文在前，证据清单在后（P04：每句的每条来源带可复制原句引文/文件名/章节/版本，
	//   且顺带给出 has_newer(资料有新版)/file_deleted(资料已删) 的可读提示——兑现卖点，不再是空壳 id）
	var sb strings.Builder
	sb.WriteString(ver.FullContent)
	sb.WriteString("\n\n---\n\n# 证据清单\n\n")

	// 按 article_sentence_id 分组 bindings
	bindMap := map[uint64][]model.EvidenceBinding{}
	for _, b := range binds {
		bindMap[b.ArticleSentenceID] = append(bindMap[b.ArticleSentenceID], b)
	}

	hasEvidence := false
	// 复用装配层把 bindings 一次读成人读 source（避免导出处逐个 SQL）
	sourceBySent := LoadSentenceSources(ctx, ver.TenantID, binds)
	for _, s := range sents {
		bs := bindMap[s.ID]
		if len(bs) == 0 {
			continue // 无绑定句不进证据清单
		}
		srcs := sourceBySent[s.ID]
		if len(srcs) == 0 {
			continue // 孤儿绑定(引用文档被物理清理)在清单中如实略过，不强编
		}
		hasEvidence = true
		sb.WriteString(fmt.Sprintf("【句子】%s\n", s.Content))
		for _, src := range srcs {
			sb.WriteString(fmt.Sprintf("  - 来源文档：%s\n", docSourceLabel(src)))
			sb.WriteString(fmt.Sprintf("    原文：%s\n", src.SourceText))
			if src.FileDeleted {
				sb.WriteString("    （该资料当前已被删除，以上为引用时的保留原文）\n")
			} else if src.HasNewer {
				sb.WriteString("    （该资料之后又有新版本，以上为引用时的旧版原文）\n")
			}
		}
		sb.WriteString("\n")
	}
	if !hasEvidence {
		sb.WriteString("（无证据）\n")
	}
	return sb.String(), nil
}

// docSourceLabel 生成导出/清单中"来源文档"一行人读标注：文件名（或文档 id 兜底）+ 章节 + 原文版本号。
func docSourceLabel(src SourceView) string {
	label := ""
	if src.FileName != "" {
		label = src.FileName
	} else if src.FileID != 0 {
		label = fmt.Sprintf("文档 %d", src.FileID)
	} else {
		label = "未知文档"
	}
	if src.ChapterTitle != "" {
		label += " · " + src.ChapterTitle
	}
	label += "（原文版本 " + src.VersionMd5 + "）"
	return label
}
