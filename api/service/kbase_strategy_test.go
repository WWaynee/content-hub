package service

import (
	"testing"

	"github.com/WWaynee/content-hub/storage"
)

// TestRRFFuseBM25ChangesOrder 验证机制：BM25 高分项能在融合排序中相对前移。
// 说明：同候选池内 RRF 的强度依赖「多个相关项共享关键词命中」——单个孤立命中项
// 与 dense 冠军的融合分几乎持平（两者各拿一路第 1 名）。真实政企 query 的关键词
// （政策编号/日期/专名）通常横跨多个相关切片，故这里构造两路关键词重叠场景。
func TestRRFFuseBM25ChangesOrder(t *testing.T) {
	pool := []storage.QdrantSearchHit{
		{Content: "统筹推进乡村全面振兴，加快农业农村现代化步伐", Score: 0.95},
		{Content: "实施乡村振兴战略，巩固拓展脱贫攻坚成果，推进乡村建设行动", Score: 0.90},
		{Content: "报名考生须具有高中毕业文化程度或同等学力", Score: 0.80},
		{Content: "报名材料包括身份证、毕业证书复印件，报名截止时间为2025年6月30日", Score: 0.75},
		{Content: "学校应当加强教师队伍建设，提高教育教学质量", Score: 0.70},
	}
	// query 的关键词（报名/文化程度/毕业/截止）只密集落在第 3、4 条
	opt := SentenceSearchOptions{TopK: 5, RRFK: 60, BM25K1: 1.2, BM25B: 0.75}
	ranked := rrfFuse("报名条件 高中毕业文化程度 报名截止时间", pool, opt)

	// 含关键词的第 3、4 条必须排在完全无关的第 5 条之前（BM25 有分项不该被零分项反超）
	idxOf := func(content string) int {
		for i, h := range ranked {
			if h.Content == content {
				return i
			}
		}
		return -1
	}
	c3, c4, c5 := idxOf(pool[2].Content), idxOf(pool[3].Content), idxOf(pool[4].Content)
	if c3 < 0 || c4 < 0 || c5 < 0 {
		t.Fatalf("候选应全部保留，实际 %d 条", len(ranked))
	}
	if c3 > c5 || c4 > c5 {
		t.Fatalf("关键词命中项(%d,%d)不应排在无关键词项(%d)之后", c3, c4, c5)
	}
}

// TestRRFFuseNoKeywordKeepsSemanticOrder 无关键词命中时融合不应打乱纯语义序。
func TestRRFFuseNoKeywordKeepsSemanticOrder(t *testing.T) {
	pool := []storage.QdrantSearchHit{
		{Content: "加强党的建设，全面从严治党，深化政治监督", Score: 0.90},
		{Content: "学校食堂卫生管理规范，保障师生饮食安全", Score: 0.80},
	}
	opt := SentenceSearchOptions{TopK: 2, RRFK: 60, BM25K1: 1.2, BM25B: 0.75}
	ranked := rrfFuse("党建监督政治工作", pool, opt)
	if ranked[0].Content != pool[0].Content {
		t.Fatalf("语义相关切片应保持第一，实际第一=%q", ranked[0].Content)
	}
}

// TestRRFFuseEmptyPool 空候选池不应 panic。
func TestRRFFuseEmptyPool(t *testing.T) {
	opt := SentenceSearchOptions{TopK: 5, RRFK: 60, BM25K1: 1.2, BM25B: 0.75}
	ranked := rrfFuse("任何查询", nil, opt)
	if len(ranked) != 0 {
		t.Fatalf("空池应返回空，实际 %d", len(ranked))
	}
}
