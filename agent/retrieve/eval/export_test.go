package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/WWaynee/content-hub/splitter"
)

// 本文件（无 build tag）提供评测的共享数据定义与离线工具：
//   - 评测类型 / doc_key 映射 / 语料清单；
//   - 句子序列重建（= doc_sentences 入库序列，规则见 testdata/README.md）；
//   - TestExportSentenceIndexes：把语料句子索引导出为 testdata/doc_sentences/<doc_key>.json，
//     供标注者对照（任务书 Step 0-A）。离线可跑，不需要 DB/LLM。

// evalTenantID 独立评测租户（与生产/其他测试租户隔离）。
const evalTenantID = uint64(95270001)

var corpusDir = filepath.Join("testdata", "corpus")

// corpusFiles 评测语料（与 testdoc/2026招生工作/ 同源，复制到 testdata 保证自包含可复现）。
var corpusFiles = []string{"去年招生数据.md", "录取规则.md", "报名条件与流程.md", "招生政策依据.md"}

// relevantSentence 一条相关句子标注（任务书格式：doc_key + 句子下标）。
type relevantSentence struct {
	Doc           string `json:"doc"`
	SentenceIndex int    `json:"sentence_index"`
}

type evalQuery struct {
	ID         string             `json:"id"`
	Query      string             `json:"query"`
	Difficulty string             `json:"difficulty"`
	Category   string             `json:"category"`
	Relevant   []relevantSentence `json:"relevant"`
	Notes      string             `json:"notes"`
}

// docKeyToFile 评测集 doc_key → 语料文件名（与 testdata/README.md 的映射一致）。
var docKeyToFile = map[string]string{
	"baoming_tiaojian": "报名条件与流程.md",
	"zhaosheng_shuju":  "去年招生数据.md",
	"luqu_guize":       "录取规则.md",
	"zhengce_yiju":     "招生政策依据.md",
}

// corpusSentences 重建文档的入库句子序列：先 splitter.Split（按结构分节 + 软上限封片），
// 再对每个 chunk 的 Content 切句后按 chunk 顺序拼接。与 ProcessDocument 写入 doc_sentences
// 的序列一致（chunk.Content 为无分隔符拼接，故以"："结尾的引导行会与后随分号列表项合句，
// 切勿对含换行的全文直接 Sentences() 数下标——见 testdata/README.md）。
func corpusSentences(text string) []string {
	var out []string
	for _, c := range splitter.Split(text, 300) {
		out = append(out, splitter.Sentences(c.Content)...)
	}
	return out
}

func loadQueries(t *testing.T) []evalQuery {
	t.Helper()
	// RETRIEVAL_EVAL_SET=sanity 时加载 5 条 sanity 集（开发期快速验证，见 testdata/README.md）
	fileName := "eval_queries.json"
	if os.Getenv("RETRIEVAL_EVAL_SET") == "sanity" {
		fileName = "eval_queries_sanity.json"
	}
	raw, err := os.ReadFile(filepath.Join(corpusDir, "..", fileName))
	if err != nil {
		t.Fatalf("读取评测集失败: %v", err)
	}
	var qs []evalQuery
	if err := json.Unmarshal(raw, &qs); err != nil {
		t.Fatalf("解析评测集失败: %v", err)
	}
	if fileName == "eval_queries.json" && len(qs) < 20 {
		t.Fatalf("评测集应 ≥20 条，实际 %d", len(qs))
	}
	if fileName == "eval_queries_sanity.json" && len(qs) < 5 {
		t.Fatalf("sanity 集应 ≥5 条，实际 %d", len(qs))
	}
	return qs
}

// TestExportSentenceIndexes 导出 4 份语料的「句子索引 → 文本」到 testdata/doc_sentences/。
func TestExportSentenceIndexes(t *testing.T) {
	outDir := filepath.Join("testdata", "doc_sentences")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for docKey, file := range docKeyToFile {
		content, err := os.ReadFile(filepath.Join(corpusDir, file))
		if err != nil {
			t.Fatal(err)
		}
		sents := corpusSentences(string(content))
		type item struct {
			Index int    `json:"index"`
			Text  string `json:"text"`
		}
		items := make([]item, len(sents))
		for i, s := range sents {
			items[i] = item{Index: i, Text: s}
		}
		raw, _ := json.MarshalIndent(items, "", "  ")
		if err := os.WriteFile(filepath.Join(outDir, docKey+".json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("导出 %s: %d 句 -> %s", docKey, len(sents), filepath.Join(outDir, docKey+".json"))
	}
}
