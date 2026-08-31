// Package writing 实现稿件撰写 agent：根据需求单 + 检索证据生成结构化稿件，
// 并在每个句子建立「句子 ↔ 证据」的绑定（写作时即建立证据对应关系）。
package writing

import (
	"context"
	"fmt"
	"strings"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/llmclient"
)

// Writer 稿件撰写 agent。
type Writer struct {
	llm llmclient.Client
}

// New 构造撰写 agent。
func New(llm llmclient.Client) *Writer { return &Writer{llm: llm} }

// llmArticle LLM 输出的结构化稿件（句子级证据引用）。
type llmArticle struct {
	Title    string `json:"title"`
	Sections []struct {
		Heading    string `json:"heading"`
		Paragraphs []struct {
			Sentences []struct {
				Text         string `json:"text"`
				EvidenceRefs []int  `json:"evidence_refs"` // 指向上文 evidence 数组的索引
			} `json:"sentences"`
		} `json:"paragraphs"`
	} `json:"sections"`
}

// Write 实现 agent.Writer。
func (w *Writer) Write(ctx context.Context, req agent.WritingRequest) (*agent.Article, error) {
	prompt := buildWritingPrompt(req)

	var out llmArticle
	if err := w.llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: prompt}}, &out); err != nil {
		return nil, fmt.Errorf("稿件生成失败: %w", err)
	}

	if strings.TrimSpace(out.Title) == "" && len(out.Sections) == 0 {
		return nil, fmt.Errorf("LLM 未生成有效稿件")
	}

	// 映射回 agent.Article，并把 evidence_refs（int 索引）转成证据 file_id 引用
	article := &agent.Article{Title: out.Title}
	for _, sec := range out.Sections {
		s := agent.Section{Heading: sec.Heading}
		for _, para := range sec.Paragraphs {
			p := agent.Paragraph{}
			for _, sent := range para.Sentences {
				refs := []uint64{}
				for _, idx := range sent.EvidenceRefs {
					if idx >= 0 && idx < len(req.Evidence) {
						refs = append(refs, uint64(idx)) // 存 evidence 数组索引
					}
				}
				p.Sentences = append(p.Sentences, agent.Sentence{Text: sent.Text, EvidenceRefs: refs})
			}
			s.Paragraphs = append(s.Paragraphs, p)
		}
		article.Sections = append(article.Sections, s)
	}
	return article, nil
}

func buildWritingPrompt(req agent.WritingRequest) string {
	// 拼接证据（带编号），让 LLM 引用编号
	var sb strings.Builder
	sb.WriteString("你是政企内容撰写助手。请根据需求单要求，结合以下检索资料，撰写一篇稿件。\n\n")
	sb.WriteString("【稿件需求】\n")
	r := req.Requirement
	sb.WriteString(fmt.Sprintf("标题：%s\n发文主体：%s\n发文目的：%s\n目标受众：%s\n基调：%s\n感情色彩：%s\n禁忌/约束：%s\n章节要求：%s\n字数要求：%d\n",
		r.Title, r.StyleSubject, r.StylePurpose, r.StyleAudience, r.StyleTone, r.StyleEmotion, r.StyleTaboo, r.ChapterRequirement, r.WordCount))
	sb.WriteString("\n【检索资料】（编号 0 起，引用时用 evidence_refs 指向编号）\n")
	for i, e := range req.Evidence {
		sb.WriteString(fmt.Sprintf("[%d] 来源：文档%d v%s %s\n%s\n", i, e.FileID, e.VersionMd5, e.ChapterTitle, e.SourceText))
	}

	sb.WriteString("\n【输出要求】只返回 JSON，结构：")
	sb.WriteString(`{"title":"...","sections":[{"heading":"...","paragraphs":[{"sentences":[{"text":"句子","evidence_refs":[编号数组]}]}]}]}`)
	sb.WriteString("\n每句话如果引用了资料，就在 evidence_refs 里列出对应编号；没有引用的句子 evidence_refs 为空数组。引用资料时尽量用原文数据/条款，不要凭空编造。")
	return sb.String()
}

// RewriteSentence 句子级重写：只重写指定句子，返回新句子文本 + 该句引用的证据索引。
// 实现 orchestrator.SentenceRewriter 接口。
func (w *Writer) RewriteSentence(ctx context.Context, req agent.WritingRequest, targetIndex int, instruction string) (string, []uint64, error) {
	// 拍平当前稿件句子，向 LLM 提供上下文
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("请重写稿件中的第 %d 个句子（从 0 起）。\n\n", targetIndex))
	sb.WriteString("【修改要求】\n" + instruction + "\n\n")
	sb.WriteString("【检索资料】（编号 0 起，引用用 evidence_refs）\n")
	for i, e := range req.Evidence {
		sb.WriteString(fmt.Sprintf("[%d] %s\n", i, e.SourceText))
	}
	sb.WriteString("\n只返回 JSON：{\"text\":\"重写后的句子\",\"evidence_refs\":[引用资料编号数组]}")
	sb.WriteString("\n如果没有引用资料，evidence_refs 为 []。不要写数据集之外的数据。")

	var out struct {
		Text         string `json:"text"`
		EvidenceRefs []int  `json:"evidence_refs"`
	}
	if err := w.llm.ChatWithJSON(ctx, []llmclient.ChatMessage{{Role: "user", Content: sb.String()}}, &out); err != nil {
		return "", nil, fmt.Errorf("句子重写失败: %w", err)
	}
	if out.Text == "" {
		return "", nil, fmt.Errorf("LLM 未返回重写句子")
	}
	refs := make([]uint64, 0, len(out.EvidenceRefs))
	for _, idx := range out.EvidenceRefs {
		if idx >= 0 && idx < len(req.Evidence) {
			refs = append(refs, uint64(idx))
		}
	}
	return out.Text, refs, nil
}
