package splitter

import (
	"strings"
	"testing"
)

// 短文档：不足软上限，应整体为单片，且无章节标题。
func TestSplit_ShortText(t *testing.T) {
	text := "这是第一句话。这是第二句话。"
	chunks := Split(text, 300)
	if len(chunks) != 1 {
		t.Fatalf("短文档应切成 1 片，实际 %d 片", len(chunks))
	}
	if chunks[0].ChapterTitle != "" {
		t.Errorf("无标题文档 ChapterTitle 应为空，实际 %q", chunks[0].ChapterTitle)
	}
	if chunks[0].Content != text {
		t.Errorf("内容应完整保留，实际 %q", chunks[0].Content)
	}
}

// 结构化文档：markdown 标题 + 中文章节标题应被识别为章节，存入 ChapterTitle。
func TestSplit_StructuredTitle(t *testing.T) {
	text := "# 招生政策\n\n## 第一章 报名条件\n本政策适用于应届毕业生。\n\n## 第二章 考试安排\n考试时间为六月中旬。"
	chunks := Split(text, 300)
	if len(chunks) == 0 {
		t.Fatal("结构化文档不应返回空")
	}
	// 至少有一个切片携带"第一章 报名条件"作为章节标题
	found := false
	for _, c := range chunks {
		if strings.Contains(c.ChapterTitle, "报名条件") {
			found = true
		}
	}
	if !found {
		t.Errorf("应提取到章节标题「第一章 报名条件」，实际 chapters=%v", chunkTitles(chunks))
	}
}

// 核心：软上限 300 字，断点落在句子中间时应延到完整句末，不得切断句子。
func TestSplit_SentenceBoundary(t *testing.T) {
	// 构造：两个长句，第一句 200 字、第二句 200 字，软上限 300。
	// 第一句(200字) + 第二句开头 100 字会到 300，但第二句不能切断，
	// 所以第一个切片只含第一句(200字)，第二句单独成片(200字)。
	s1 := strings.Repeat("甲", 199) + "。"
	s2 := strings.Repeat("乙", 199) + "。"
	chunks := Split(s1+s2, 300)
	if len(chunks) != 2 {
		t.Fatalf("应切成 2 片，实际 %d 片", len(chunks))
	}
	if chunks[0].Content != s1 {
		t.Errorf("第 1 片应完整保留第一句，实际长度 %d", len([]rune(chunks[0].Content)))
	}
	if chunks[1].Content != s2 {
		t.Errorf("第 2 片应完整保留第二句，实际长度 %d", len([]rune(chunks[1].Content)))
	}
	// 每片都必须以句末标点结尾（完整句）
	for i, c := range chunks {
		rs := []rune(c.Content)
		if !isSentenceEnd(rs[len(rs)-1]) {
			t.Errorf("chunk[%d] 未以完整句末结尾: %q", i, c.Content)
		}
	}
}

// 超长单句：软上限语义下不硬切，完整保留（超过上限是允许的）。
func TestSplit_LongSingleSentence(t *testing.T) {
	long := strings.Repeat("丙", 500) + "。"
	chunks := Split(long, 300)
	if len(chunks) != 1 {
		t.Fatalf("超长单句应完整保留为 1 片，实际 %d 片", len(chunks))
	}
	if !strings.HasSuffix(chunks[0].Content, "。") {
		t.Errorf("超长单句应以句号结尾")
	}
}

// 空文本 / 纯空白：返回 nil。
func TestSplit_Empty(t *testing.T) {
	if got := Split("", 300); got != nil {
		t.Errorf("空文本应返回 nil，实际 %v", got)
	}
	if got := Split("   \n  ", 300); got != nil {
		t.Errorf("纯空白应返回 nil，实际 %v", got)
	}
}

// 内容完整性：正文内容拼接后应保留原文所有非空白、非标题行字符。
// 标题行作为 ChapterTitle 元信息单独存储，不重复进入正文 Content（这是证据语义的正确取舍）。
func TestSplit_NoContentLoss(t *testing.T) {
	// 无标题的纯正文，保证 Content 拼接后 = 原文非空白
	text := "第一段第一句。第一段第二句。\n\n第二段内容很长" +
		strings.Repeat("测试内容。", 30) + "结尾。"
	chunks := Split(text, 100)
	var joined strings.Builder
	for _, c := range chunks {
		joined.WriteString(c.Content)
	}
	want := stripWhitespace(text)
	got := stripWhitespace(joined.String())
	if want != got {
		t.Errorf("切片拼接后正文内容丢失：want=%q got=%q", want, got)
	}
}

// TestSplit_TitleGoesToMetadata 标题行应进入 ChapterTitle 元信息，而不重复进正文。
func TestSplit_TitleGoesToMetadata(t *testing.T) {
	text := "## 第二章 说明\n这是正文内容。"
	chunks := Split(text, 300)
	if len(chunks) != 1 {
		t.Fatalf("应切 1 片，实际 %d", len(chunks))
	}
	if chunks[0].ChapterTitle != "## 第二章 说明" {
		t.Errorf("ChapterTitle 应为标题行，实际 %q", chunks[0].ChapterTitle)
	}
	if !strings.Contains(chunks[0].Content, "正文内容") {
		t.Errorf("正文应含实际内容，实际 %q", chunks[0].Content)
	}
	if strings.Contains(chunks[0].Content, "第二章") {
		t.Errorf("标题不应重复进入正文 Content，实际 %q", chunks[0].Content)
	}
}

func stripWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
}

// size <= 0 时使用 DefaultSize。
func TestSplit_DefaultSize(t *testing.T) {
	text := strings.Repeat("短句。", 100) // 300 字
	chunks := Split(text, 0)
	if len(chunks) == 0 {
		t.Fatal("不应返回空")
	}
}

func chunkTitles(chunks []Chunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.ChapterTitle
	}
	return out
}
