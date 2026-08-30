// Package splitter 提供文档文本切片（Chunking）能力。
//
// content-hub 的切片策略与通用 RAG 不同，核心诉求是「证据溯源」：
//   - 切片必须是完整句子，绝不在句子中间切断（保证引用原文语义完整）；
//   - 优先按文档结构（markdown 标题 / 中文章节条款）分节，并提取章节标题存入元信息；
//   - 字数上限是「软上限」：累加到接近上限后，遇到完整句末才封片，允许略微超出。
//
// 这样做的原因：证据清单需要展示「原文原话片段」，而一个被拦腰切断的句子
// 无法作为可信的引用来源。
package splitter

import (
	"regexp"
	"strings"
)

// DefaultSize 单个切片默认软上限（字符数）。可按配置覆盖。
const DefaultSize = 300

// Chunk 一个切片。
type Chunk struct {
	Content      string // 切片原文（完整句末截断，不含人工加工）
	ChapterTitle string // 所属章节标题；提取不到为空串
}

// 标题识别正则（结构化文档）。
var (
	markdownTitleRe = regexp.MustCompile(`^\s*#{1,6}\s+`)
	cnTitleRe       = regexp.MustCompile(`^\s*第\s*[一二三四五六七八九十百千万0-9]+\s*(章|条|节|款|部分|篇)`)
	sectionRe       = regexp.MustCompile(`^\s*(附录|附件|前言|引言)`)
)

// isTitle 判断一行是否为「结构标题行」。
func isTitle(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	if markdownTitleRe.MatchString(t) {
		return true
	}
	if cnTitleRe.MatchString(t) {
		return true
	}
	if sectionRe.MatchString(t) {
		return true
	}
	return false
}

// isSentenceEnd 判断 rune 是否为句末标点（用于按完整句切分）。
// 中文句末标点 + 英文句末标点 + 换行（段落内换行也算一个句子边界）。
func isSentenceEnd(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '!', '?', ';', '\n', '\r':
		return true
	}
	return false
}

// Sentences 把一段文本按句末标点切成完整句子（保留标点），供证据绑定层复用。
func Sentences(text string) []string {
	return splitSentences(text)
}

// splitSentences 把一段文本按句末标点切成完整句子（保留标点）。
func splitSentences(text string) []string {
	runes := []rune(text)
	var sents []string
	start := 0
	for i, r := range runes {
		if isSentenceEnd(r) {
			sents = append(sents, string(runes[start:i+1]))
			start = i + 1
		}
	}
	if start < len(runes) {
		sents = append(sents, string(runes[start:]))
	}
	return sents
}

// Split 把整篇文本切成切片。
//
// 算法：
//  1. 按行扫描，识别结构标题行，开启一个新「节」；节标题作为该节所有切片的 ChapterTitle。
//  2. 每节的正文按完整句切分，逐句累加；当累计长度超过 soft 上限时，在「上一句的句末」封片。
//  3. 单个句子本身超过上限时不做硬切，完整保留（软上限语义）。
//
// size <= 0 时使用 DefaultSize。
func Split(text string, size int) []Chunk {
	if size <= 0 {
		size = DefaultSize
	}

	var chunks []Chunk
	var currentTitle string
	var pending []string // 当前节待封片的完整句子

	flush := func() {
		chunks = append(chunks, assemble(pending, currentTitle, size)...)
		pending = nil
	}

	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if isTitle(t) {
			flush()
			currentTitle = t
			continue
		}
		pending = append(pending, splitSentences(line)...)
	}
	flush()

	if len(chunks) == 0 {
		return nil
	}
	return chunks
}

// assemble 把一组完整句子按软上限拼成切片，每个切片带章节标题。
func assemble(sents []string, title string, size int) []Chunk {
	var chunks []Chunk
	var cur []string
	curLen := 0

	for _, s := range sents {
		slen := len([]rune(s))
		// 已有内容且再加这句会超过软上限 → 封片（在上一句句末截断）
		if curLen > 0 && curLen+slen > size {
			chunks = append(chunks, Chunk{Content: strings.Join(cur, ""), ChapterTitle: title})
			cur = nil
			curLen = 0
		}
		cur = append(cur, s)
		curLen += slen
	}
	if len(cur) > 0 {
		chunks = append(chunks, Chunk{Content: strings.Join(cur, ""), ChapterTitle: title})
	}
	return chunks
}
