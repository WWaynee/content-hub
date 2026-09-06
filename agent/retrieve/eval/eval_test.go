//go:build integration

// Package eval 是 P14 的检索质量评测基准：
//   - 语料：testdata/corpus/ 下 4 份招生政策 md（入库到独立评测租户，可重复跑）；
//   - 标注：testdata/eval_queries.json 共 20 条 query（easy 8 / medium 8 / hard 4），
//     ground truth 以 (doc_key, sentence_index) 标注（规则见 testdata/README.md）；
//   - 指标：Recall@3/5/10、Precision@3/5/10、MRR、NDCG@5，输出 markdown 对比表。
//
// 运行（需 MySQL/Qdrant/OSS 与 LLM/Embedding/Rerank 真实 key 配置齐）：
//
//	# 单策略（dense | hybrid | hybrid_rerank），配合 RETRIEVAL_QUERY_EXPAND=1 开查询扩展
//	RETRIEVAL_STRATEGY=dense go test ./agent/retrieve/eval/ -run TestRetrievalQuality -v -count=1 -tags=integration
//
//	# 全部策略一键对比（默认全跑，含 LLM 查询扩展，耗时较长）
//	go test ./agent/retrieve/eval/ -run TestAllStrategiesComparison -v -count=1 -tags=integration -timeout 30m
package eval

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/WWaynee/content-hub/agent/retrieve"
	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// evalEnv 一次评测运行的环境：语料已入库、ground truth 已定位到 doc_sentence_id。
type evalEnv struct {
	tenantID uint64
	// fileIDs 按 corpusFiles 顺序
	fileIDs []uint64
	// sentenceIDs[file] = 该文件全部 doc_sentence 的 id（按 chunk/sentence 顺序，与 corpusSentences 对齐）
	sentenceIDs map[string][]uint64
	// queries 评测集
	queries []evalQuery
	// groundTruth[qIdx] 相关 doc_sentence_id 集合
	groundTruth [][]uint64
}

// setupEval 确保语料入库（复用已存在的同名文件，幂等）并把 ground truth 定位到句子 ID。
func setupEval(t *testing.T) *evalEnv {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if cfg.Embedding.APIKey == "" || cfg.LLM.APIKey == "" || cfg.OSS.AccessKeyID == "" {
		t.Skip("LLM/Embedding/OSS 未配置真实 key，跳过")
	}
	if storage.OSSClient == nil {
		_ = storage.InitOSS()
	}
	if storage.QdrantClient == nil {
		if err := storage.InitQdrant(4096); err != nil {
			t.Skipf("Qdrant 不可用: %v", err)
		}
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}

	ctx := context.Background()
	env := &evalEnv{tenantID: evalTenantID, sentenceIDs: map[string][]uint64{}}

	// 1. 已有文件（上次运行遗留）→ 校验完整性后复用；缺失或不完整 → IngestAndParse 同步入库（需 OSS）
	existing, err := storage.ListAllFiles(ctx, evalTenantID, storage.ScopePublic, 0)
	if err != nil {
		t.Fatalf("列出评测租户文件失败: %v", err)
	}
	byName := map[string]uint64{}
	for _, f := range existing {
		byName[f.Name] = f.ID
	}
	for _, name := range corpusFiles {
		id, ok := byName[name]
		complete := false
		if ok {
			// 校验完整性：存在 latest 版本且已切分出句子（防上次失败残留的"空文件"被误复用）
			if ver, verr := storage.GetLatestVersion(ctx, id); verr == nil {
				if chunks, cerr := storage.ListChunksByVersion(ctx, evalTenantID, id, ver.VersionMd5); cerr == nil && len(chunks) > 0 {
					complete = true
				}
			}
			if !complete {
				// 残留空文件：软删后重建（向量点尚未写入时无残留；重复跑安全）
				_ = storage.SoftDeleteFile(ctx, evalTenantID, storage.ScopePublic, 0, id)
				delete(byName, name)
				ok = false
			}
		}
		if !ok {
			content, rerr := os.ReadFile(filepath.Join(corpusDir, name))
			if rerr != nil {
				t.Fatalf("读取语料 %s 失败: %v", name, rerr)
			}
			res, ierr := service.IngestAndParse(ctx, service.IngestParams{
				TenantID:    evalTenantID,
				Scope:       storage.ScopePublic,
				OwnerUserID: 0,
				DirID:       0,
				FileName:    name,
				Content:     content,
			})
			if ierr != nil {
				t.Fatalf("入库语料 %s 失败: %v", name, ierr)
			}
			id = res.FileID
		}
		env.fileIDs = append(env.fileIDs, id)

		// 2. 取该文件全部句子（与 corpusSentences 对齐校验）
		ver, verr := storage.GetLatestVersion(ctx, id)
		if verr != nil {
			t.Fatalf("查询 %s 最新版本失败: %v", name, verr)
		}
		chunks, cerr := storage.ListChunksByVersion(ctx, evalTenantID, id, ver.VersionMd5)
		if cerr != nil {
			t.Fatalf("查询 %s 切片失败: %v", name, cerr)
		}
		var dbSents []model.DocSentence
		for _, c := range chunks {
			sents, serr := storage.ListSentencesByChunk(ctx, c.ID)
			if serr != nil {
				t.Fatalf("查询 %s 句子失败: %v", name, serr)
			}
			dbSents = append(dbSents, sents...)
		}
		content, rerr := os.ReadFile(filepath.Join(corpusDir, name))
		if rerr != nil {
			t.Fatalf("读取语料 %s 失败: %v", name, rerr)
		}
		expected := corpusSentences(string(content))
		if len(expected) != len(dbSents) {
			t.Fatalf("%s 入库句子数(%d)与本地重建(%d)不一致，评测基准失真", name, len(dbSents), len(expected))
		}
		ids := make([]uint64, len(dbSents))
		for i := range dbSents {
			ids[i] = dbSents[i].ID
		}
		env.sentenceIDs[name] = ids
	}

	// 3. 加载评测集并把 (doc_key, sentence_index) 定位到 doc_sentence_id。
	//    句子序列 = corpusSentences 重建（= doc_sentences 入库序列，setup 上面已校验长度一致）。
	env.queries = loadQueries(t)
	for _, q := range env.queries {
		var rel []uint64
		for _, r := range q.Relevant {
			file, ok := docKeyToFile[r.Doc]
			if !ok {
				t.Fatalf("query %s 标注了未知 doc_key %q", q.ID, r.Doc)
			}
			ids := env.sentenceIDs[file]
			if r.SentenceIndex < 0 || r.SentenceIndex >= len(ids) {
				t.Fatalf("query %s 的 sentence_index %d 超出 %s 句子范围(%d)", q.ID, r.SentenceIndex, file, len(ids))
			}
			rel = append(rel, ids[r.SentenceIndex])
		}
		if len(rel) == 0 {
			t.Fatalf("query %s 没有标注任何相关句", q.ID)
		}
		env.groundTruth = append(env.groundTruth, rel)
	}
	return env
}

// baseOpts 评测统一检索参数：与生产默认一致，方便复现。
func baseOpts() service.SentenceSearchOptions {
	return service.DefaultSentenceSearchOptions()
}

// qMetrics 单条 query 的检索质量指标。
type qMetrics struct {
	Recall    map[int]float64
	Precision map[int]float64
	MRR       float64
	NDCG5     float64
}

var evalKs = []int{3, 5, 10}

// computeMetrics 依据相关集与排序结果计算指标。
func computeMetrics(rel []uint64, ranked []uint64) qMetrics {
	relSet := map[uint64]bool{}
	for _, id := range rel {
		relSet[id] = true
	}
	pos := map[uint64]int{}
	for i, id := range ranked {
		if _, ok := pos[id]; !ok {
			pos[id] = i
		}
	}
	m := qMetrics{Recall: map[int]float64{}, Precision: map[int]float64{}}
	for _, k := range evalKs {
		hit := 0
		if len(ranked) < k {
			k = len(ranked) // K 超过实际返回数时按实际算
		}
		for i := 0; i < k && i < len(ranked); i++ {
			if relSet[ranked[i]] {
				hit++
			}
		}
		if len(rel) > 0 {
			m.Recall[k] = float64(hit) / float64(len(rel))
		}
		if k > 0 {
			m.Precision[k] = float64(hit) / float64(k)
		}
	}
	// MRR
	first := -1
	for i, id := range ranked {
		if relSet[id] {
			first = i
			break
		}
	}
	if first >= 0 {
		m.MRR = 1.0 / float64(first+1)
	}
	// NDCG@5（二元相关）
	k5 := 5
	if len(ranked) < k5 {
		k5 = len(ranked)
	}
	var dcg, idcg float64
	for i := 0; i < k5; i++ {
		if relSet[ranked[i]] {
			dcg += 1.0 / math.Log2(float64(i+2))
		}
	}
	nRel := len(rel)
	for i := 0; i < k5 && i < nRel; i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}
	if idcg > 0 {
		m.NDCG5 = dcg / idcg
	}
	return m
}

// aggregate 多 query 平均。
type aggregate struct {
	name    string
	count   int
	byDiff  map[string]int // 各难度 query 数
	sums    map[string]qMetrics
	recall5ByDiff map[string]float64
}

func newAggregate(name string) *aggregate {
	return &aggregate{
		name: name, byDiff: map[string]int{}, sums: map[string]qMetrics{},
		recall5ByDiff: map[string]float64{},
	}
}

func (a *aggregate) add(diff string, m qMetrics) {
	a.count++
	a.byDiff[diff]++
	s := a.sums[diff]
	if s.Recall == nil {
		s = qMetrics{Recall: map[int]float64{}, Precision: map[int]float64{}}
	}
	for _, k := range evalKs {
		s.Recall[k] += m.Recall[k]
		s.Precision[k] += m.Precision[k]
	}
	s.MRR += m.MRR
	s.NDCG5 += m.NDCG5
	a.sums[diff] = s
}

func (a *aggregate) avg() qMetrics {
	out := qMetrics{Recall: map[int]float64{}, Precision: map[int]float64{}}
	n := a.count
	for _, k := range evalKs {
		var rSum, pSum float64
		for _, s := range a.sums {
			rSum += s.Recall[k]
			pSum += s.Precision[k]
		}
		out.Recall[k] = rSum / float64(n)
		out.Precision[k] = pSum / float64(n)
	}
	for _, s := range a.sums {
		out.MRR += s.MRR
		out.NDCG5 += s.NDCG5
	}
	out.MRR /= float64(n)
	out.NDCG5 /= float64(n)
	return out
}

func (a *aggregate) avgByDiff(diff string) qMetrics {
	// 按难度平均
	out := qMetrics{Recall: map[int]float64{}, Precision: map[int]float64{}}
	n := a.byDiff[diff]
	if n == 0 {
		return out
	}
	s := a.sums[diff]
	for _, k := range evalKs {
		out.Recall[k] = s.Recall[k] / float64(n)
		out.Precision[k] = s.Precision[k] / float64(n)
	}
	out.MRR = s.MRR / float64(n)
	out.NDCG5 = s.NDCG5 / float64(n)
	return out
}

// searchOnce 执行一次策略检索，返回有序 doc_sentence_id。
func searchOnce(ctx context.Context, env *evalEnv, query string, opt service.SentenceSearchOptions) ([]uint64, error) {
	hits, err := service.SearchSentencesByStrategy(ctx, env.tenantID, query, opt, env.fileIDs...)
	if err != nil {
		return nil, err
	}
	out := make([]uint64, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.DocSentenceID)
	}
	return out, nil
}

// runCase 跑一个策略组合（含可选的 LLM 查询扩展）。
func runCase(ctx context.Context, t *testing.T, env *evalEnv, name string, opt service.SentenceSearchOptions, expand bool) *aggregate {
	t.Helper()
	agg := newAggregate(name)
	llm := llmclient.NewClient()
	for qi, q := range env.queries {
		var ranked []uint64
		if expand {
			// 查询扩展：原 query 检索结果保序在前（精度不被稀释），扩展 query 只做补充召回——
			// 与 guardian.Judge 多轮累积语义一致。补充部分按「被扩展 query 命中次数降序、最高分降序」。
			primary, err := searchOnce(ctx, env, q.Query, opt)
			if err != nil {
				t.Fatalf("query %s 检索失败: %v", q.ID, err)
			}
			ranked = primary
			seen := map[uint64]bool{}
			for _, id := range ranked {
				seen[id] = true
			}
			type aggHit struct {
				id   uint64
				n    int
				best float32
			}
			byID := map[uint64]*aggHit{}
			var order []uint64
			for _, eq := range retrieve.ExpandQuery(ctx, llm, q.Query, 3) {
				hits, err := service.SearchSentencesByStrategy(ctx, env.tenantID, eq, opt, env.fileIDs...)
				if err != nil {
					t.Fatalf("query %s 扩展检索失败(%q): %v", q.ID, eq, err)
				}
				for _, h := range hits {
					if seen[h.DocSentenceID] {
						continue
					}
					a, ok := byID[h.DocSentenceID]
					if !ok {
						a = &aggHit{id: h.DocSentenceID}
						byID[h.DocSentenceID] = a
						order = append(order, h.DocSentenceID)
					}
					a.n++
					if h.Score > a.best {
						a.best = h.Score
					}
				}
			}
			sort.Slice(order, func(i, j int) bool {
				a, b := byID[order[i]], byID[order[j]]
				if a.n != b.n {
					return a.n > b.n
				}
				return a.best > b.best
			})
			for _, id := range order {
				ranked = append(ranked, id)
			}
		} else {
			var err error
			ranked, err = searchOnce(ctx, env, q.Query, opt)
			if err != nil {
				t.Fatalf("query %s 检索失败: %v", q.ID, err)
			}
		}
		m := computeMetrics(env.groundTruth[qi], ranked)
		agg.add(q.Difficulty, m)
		if testing.Verbose() {
			t.Logf("  [%s] %s(%s) MRR=%.3f NDCG@5=%.3f R@5=%.2f 相关=%d 返回=%d",
				name, q.ID, q.Difficulty, m.MRR, m.NDCG5, m.Recall[5], len(env.groundTruth[qi]), len(ranked))
		}
	}
	return agg
}

// TestRetrievalQuality 单策略评测（env：RETRIEVAL_STRATEGY=dense|hybrid|hybrid_rerank，
// RETRIEVAL_QUERY_EXPAND=1 叠加查询扩展；默认 dense 基线）。
func TestRetrievalQuality(t *testing.T) {
	env := setupEval(t)
	strategy := service.SearchStrategy(config.Get().Retrieval.Strategy)
	if strategy == "" {
		strategy = service.StrategyDense
	}
	expand := config.Get().Retrieval.QueryExpandEnable
	name := string(strategy)
	if expand {
		name += "+query_expand"
	}
	opt := baseOpts()
	opt.Strategy = strategy
	agg := runCase(context.Background(), t, env, name, opt, expand)
	printResult(t, agg)
}

// TestAllStrategiesComparison 全部策略一键对比（输出 markdown 表）。
func TestAllStrategiesComparison(t *testing.T) {
	env := setupEval(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		strat  service.SearchStrategy
		expand bool
	}{
		{"dense", service.StrategyDense, false},
		{"hybrid", service.StrategyHybrid, false},
		{"hybrid_rerank", service.StrategyHybridRerank, false},
		{"hybrid_rerank+query_expand", service.StrategyHybridRerank, true},
	}
	var aggs []*aggregate
	for _, c := range cases {
		opt := baseOpts()
		opt.Strategy = c.strat
		t.Logf("== 运行策略 %s ==", c.name)
		aggs = append(aggs, runCase(ctx, t, env, c.name, opt, c.expand))
	}
	printComparison(t, aggs)
}

func printResult(t *testing.T, agg *aggregate) {
	t.Helper()
	a := agg.avg()
	t.Logf("\n=== 策略 %s 平均指标 ===\n", agg.name)
	t.Logf("| 指标 | 值 |")
	t.Logf("|---|---|")
	for _, k := range evalKs {
		t.Logf("| Recall@%d | %.3f |", k, a.Recall[k])
	}
	for _, k := range evalKs {
		t.Logf("| Precision@%d | %.3f |", k, a.Precision[k])
	}
	t.Logf("| MRR | %.3f |", a.MRR)
	t.Logf("| NDCG@5 | %.3f |", a.NDCG5)
}

func printComparison(t *testing.T, aggs []*aggregate) {
	t.Helper()
	avgs := make([]qMetrics, len(aggs))
	names := make([]string, len(aggs))
	for i, agg := range aggs {
		names[i] = agg.name
		avgs[i] = agg.avg()
	}
	var b strings.Builder
	b.WriteString("\n=== P14 检索策略对比（20 query × 3 难度，评测集 agent/retrieve/eval）===\n")
	hdr := "| 指标 | " + strings.Join(names, " | ") + " |"
	sep := "|" + strings.Repeat("---|", len(names)+1)
	b.WriteString(hdr + "\n" + sep + "\n")
	for _, k := range evalKs {
		row := fmt.Sprintf("| Recall@%d |", k)
		for _, a := range avgs {
			row += fmt.Sprintf(" %.3f |", a.Recall[k])
		}
		b.WriteString(row + "\n")
	}
	for _, k := range evalKs {
		row := fmt.Sprintf("| Precision@%d |", k)
		for _, a := range avgs {
			row += fmt.Sprintf(" %.3f |", a.Precision[k])
		}
		b.WriteString(row + "\n")
	}
	row := "| MRR |"
	for _, a := range avgs {
		row += fmt.Sprintf(" %.3f |", a.MRR)
	}
	b.WriteString(row + "\n")
	row = "| NDCG@5 |"
	for _, a := range avgs {
		row += fmt.Sprintf(" %.3f |", a.NDCG5)
	}
	b.WriteString(row + "\n")
	t.Log(b.String())

	// 难度分组
	for _, diff := range []string{"easy", "medium", "hard"} {
		var db strings.Builder
		db.WriteString(fmt.Sprintf("\n--- %s 难度（Recall@5 / Precision@5 / MRR / NDCG@5）---\n", diff))
		db.WriteString("| 策略 | Recall@5 | Precision@5 | MRR | NDCG@5 |\n|---|---|---|---|---|\n")
		for i, agg := range aggs {
			a := agg.avgByDiff(diff)
			db.WriteString(fmt.Sprintf("| %s | %.3f | %.3f | %.3f | %.3f |\n",
				names[i], a.Recall[5], a.Precision[5], a.MRR, a.NDCG5))
		}
		t.Log(db.String())
	}

	// 对比结论：以 dense 为基线的提升（任务书 §5.2 要求至少一个策略 Recall@5 提升 ≥5pp）
	base := avgs[0]
	for i := 1; i < len(avgs); i++ {
		t.Logf("== %s vs %s：Recall@5 %+.1fpp, Precision@5 %+.1fpp, MRR %+.3f, NDCG@5 %+.3f ==",
			names[i], names[0],
			(avgs[i].Recall[5]-base.Recall[5])*100,
			(avgs[i].Precision[5]-base.Precision[5])*100,
			avgs[i].MRR-base.MRR, avgs[i].NDCG5-base.NDCG5)
	}
}
