package service

import (
	"context"
	"sort"
	"strings"

	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// sources.go — P04：把证据绑定(一串 doc_sentence_id)装配成「人读 source」，
// 供 GET /article 的 sentence_views / tooltip / 导出证据清单使用。
//
// RFC 出处：rev-2 §8.2-Q2 / §10.1、rev-4 §13.6 / W6。
//
// 组装结果：{doc_sentence_id, source_text, file_id, file_name, scope, chapter_title,
// version_md5, has_newer, file_deleted}，其中
//   - has_newer   = 绑定引用的版本 md5 ≠ 文档当前版本 md5（资料之后又更新过 → 用户侧应有"已有新版"提示）
//   - file_deleted= 绑定指向的文档当前已被删除(active=0)，但仍可还原其文件名/原文（不可变锚），仅提示其已不可见
//   - 找不到原文句/文档的孤儿绑定会被安全忽略（不回滚整份稿件，只少一条不强编）

// SourceView 一条 evidence binding 展开后的人读引用来源。
type SourceView struct {
	DocSentenceID uint64 `json:"doc_sentence_id"`
	SourceText    string `json:"source_text"`
	FileID        uint64 `json:"file_id"`
	FileName      string `json:"file_name"`
	Scope         string `json:"scope,omitempty"`
	ChapterTitle  string `json:"chapter_title,omitempty"`
	VersionMd5    string `json:"version_md5"`
	HasNewer      bool   `json:"has_newer"`
	FileDeleted   bool   `json:"file_deleted"`
}

// 声明态(claim_type)常量，对齐 rev-2 §10.1。
const (
	ClaimTypeBound       = "bound"        // 有外部引用，可溯源
	ClaimTypePlausibleAI = "plausible-ai" // 纯 AI 通稿语，无外部引用
	// no_source（疑似该有据却无源、需人工复核标黄）由 P09 治理层填充，P04 不产生
)

// LoadSentenceSources 把一个版本的 bindings 装配成人读 source。
// 返回 map: article_sentence_id → []SourceView（保持 bindings 每条先后序/order_no 稳定）。
// 数据取自批量查询，避免对每条 evidence 单独 SQL（P04 从"join"起步、量大后再做快照冗余）。
func LoadSentenceSources(ctx context.Context, tenantID uint64, bindings []model.EvidenceBinding) map[uint64][]SourceView {
	out := make(map[uint64][]SourceView)
	if len(bindings) == 0 {
		return out
	}

	// 收集去重的 doc_sentence / doc_file / doc_chunk id，且保留绑定本来的先后序
	orderBySentence := map[uint64][]model.EvidenceBinding{}
	docSentIDIdx := map[uint64]struct{}{}
	fileIDIdx := map[uint64]struct{}{}
	chunkIDIdx := map[uint64]struct{}{}
	for _, b := range bindings {
		if b.DocSentenceID == 0 {
			continue
		}
		orderBySentence[b.ArticleSentenceID] = append(orderBySentence[b.ArticleSentenceID], b)
		docSentIDIdx[b.DocSentenceID] = struct{}{}
		if b.DocFileID != 0 {
			fileIDIdx[b.DocFileID] = struct{}{}
		}
	}
	if len(docSentIDIdx) == 0 {
		return out
	}

	// 批量取原文句、切片(章节标题)、文档元数据（含已软删文件也能拿到 name/current_version_md5）。
	// 存储错误不阻断整份稿件：该批命中为空时对应 source 会自然退回"仅 doc/孤儿"可读形态，
	// 属只读装配的降级而非主链路失败；不会因一次 join 查询抖动把整篇 sentence_views 打掉。
	sents, _ := storage.ListDocSentencesByIDs(ctx, tenantID, mapKeys(docSentIDIdx))
	sentByID := make(map[uint64]model.DocSentence, len(sents))
	for _, s := range sents {
		sentByID[s.ID] = s
		if s.ChunkID != 0 {
			chunkIDIdx[s.ChunkID] = struct{}{}
		}
		if s.FileID != 0 {
			fileIDIdx[s.FileID] = struct{}{}
		}
	}
	chunks, _ := storage.ListDocChunksByIDs(ctx, tenantID, mapKeys(chunkIDIdx))
	chunkByID := make(map[uint64]model.DocChunk, len(chunks))
	for _, c := range chunks {
		chunkByID[c.ID] = c
	}
	files, _ := storage.ListFilesByIDs(ctx, tenantID, mapKeys(fileIDIdx))
	fileByID := make(map[uint64]model.KbaseFile, len(files))
	for _, f := range files {
		fileByID[f.ID] = f
	}

	for sentenceID, bs := range orderBySentence {
		sources := make([]SourceView, 0, len(bs))
		for _, b := range bs {
			s, ok := sentByID[b.DocSentenceID]
			if !ok {
				continue // 孤儿绑定：原文已不存在 → 安全忽略该条，不强编
			}
			// 归属文档：优先取绑定 file_id 命中的文件，缺失时回落到句自身 file_id
			f, fok := fileByID[b.DocFileID]
			if !fok {
				f, fok = fileByID[s.FileID]
			}
			src := SourceView{
				DocSentenceID: s.ID,
				SourceText:    strings.TrimSpace(s.Content),
				FileID:        s.FileID,
				VersionMd5:    s.VersionMd5,
			}
			if fok {
				src.FileName = f.Name
				src.Scope = f.Scope
				src.HasNewer = f.CurrentVersionMd5 != "" && s.VersionMd5 != f.CurrentVersionMd5
				src.FileDeleted = f.Active == 0
			}
			if c, ok := chunkByID[s.ChunkID]; ok {
				src.ChapterTitle = c.ChapterTitle
			}
			sources = append(sources, src)
		}
		out[sentenceID] = sources
	}
	return out
}

// SentenceView 是逐句的人读视图（RFC rev-2 §10.1）：
//   - 有 sources  → claim_type=bound（有据可溯源）
//   - 无 sources  → claim_type=plausible-ai（纯 AI 通稿语，sources 为空）
// 是否"疑似该有据却无"的 no_source/需人工态无法仅凭 binding 存在与否推断，
// 交由 P09 治理层填充——P04 先给基础两态占位，与 RFC「sources=[] + claim_type」对齐。
type SentenceView struct {
	ArticleSentenceID uint64       `json:"sentence_id"`
	Text              string       `json:"text"`
	ClaimType         string       `json:"claim_type"`
	Sources           []SourceView `json:"sources"`
}

// BuildSentenceViews 把某版本句子 + 对应 sources 组装成结构化的 sentence_views 列表
// （顺序沿用调用方传入的句子顺序，通常是 ListArticleSentences 的结构化升序）。无 binding 句给 plausible-ai。
func BuildSentenceViews(sents []model.ArticleSentence, sourceBySent map[uint64][]SourceView) []SentenceView {
	views := make([]SentenceView, 0, len(sents))
	for _, s := range sents {
		srcs := sourceBySent[s.ID]
		ct := ClaimTypePlausibleAI
		if len(srcs) > 0 {
			ct = ClaimTypeBound
		}
		views = append(views, SentenceView{
			ArticleSentenceID: s.ID,
			Text:              s.Content,
			ClaimType:         ct,
			Sources:           srcs,
		})
	}
	return views
}

// mapKeys 有序抽取 map[uint64]struct{} 的键（确定性，便于批量查询）。
func mapKeys(m map[uint64]struct{}) []uint64 {
	keys := make([]uint64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
