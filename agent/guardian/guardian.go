// Package guardian 实现单个子需求点(claim)的「检索—裁决」小循环与三态裁决。
//
// P06：让"检索不足就换 query、仍不足就报缺证给人（而非硬抛）"成为一个带 hard-budget 的
// 运行时决策(Guardian)，从而在 run 里能体现 A1(自主换路)/A3(预算兜底不硬抛)：
//   accept   → 该 claim 证据覆盖足够，可交给 Writer；
//   retry    → 覆盖不足但还有换角度空间，给一条新 query 再去检索（不重复）；
//   ask_human→ 无论怎么换都无源，把缺证收敛成一条待确认交给用户（而非直接 Err... 外层红）。
//
// 本包不含任何 service/storage 依赖，只依赖注入的 Searcher 与 Thinker，方便无 LLM/无网单测。
package guardian

import (
	"context"
	"errors"
	"fmt"

	"github.com/WWaynee/content-hub/agent"
)

// ErrBudget 无源且已用尽 budget（用作 ask_human 的客观兜底，不当作硬 throw 使用）。
var ErrBudget = errors.New("检索预算用尽仍未获得该点支撑")

// SearchFn 一次句子级检索（注入：生产用 service.SearchKbaseSentences，测试用 fake 计数）。
type SearchFn func(ctx context.Context, tenantID uint64, query string, fileIDs ...uint64) ([]agent.Evidence, error)

// ThinkFn 为某个未覆盖的 claim 提出下一条候选 query（需排除已试过的 low/no-hit query）。
// 返回 (query, true) 表示有可取的新角度；false 表示想不出新 query（→ guardian 判 ask_human）。
type ThinkFn func(ctx context.Context, claim string, triedQueries []string) (string, bool, error)

// Decision 对单个 claim 的裁决。
type Decision struct {
	// Verdict accept | retry | ask_human
	Verdict string
	Claim   string
	Reason  string
	// Tried 本 claim 本轮实际投入的 query（含重复剔除，审计用）。
	Tried []string
	// Evidence 到 accept 那一刻累计命中的证据（供 Writer 写这句）。
	Evidence []agent.Evidence
	// Missing 缺证描述（ask_human 用，给用户一句人话）。
	Missing []string
}

// 裁决值
const (
	VerdictAccept    = "accept"
	VerdictRetry     = "retry"
	VerdictAskHuman  = "ask_human"
)

// Options 一次裁决的参数。
type Options struct {
	TenantID uint64
	ScopeIDs []uint64 // fileIDs（空=全租户）
	Budget   int      // 硬预算：本 claim 最多尝试几次检索；<=0 用默认 3
	// MinCover 判"已能支撑"的最少可命中条数（默认 1）。若该 claim 一次命中即算覆盖。
	MinCover int
	Search   SearchFn
	Think    ThinkFn
	// Claim 识别名（给缺证人话/审计）。
	Claim string
}

// Judge 对单个 claim 执行 带预算的 检索→评→换 query 循环，返回三态之一。
// 保证：检索次数 <= Budget；候选 query 不重复；无源最终是 ask_human，而非硬抛外层错误。
func Judge(ctx context.Context, opt Options) (*Decision, error) {
	if opt.Budget <= 0 {
		opt.Budget = 3
	}
	if opt.MinCover <= 0 {
		opt.MinCover = 1
	}
	if opt.Search == nil {
		return nil, errors.New("guardian: 需要 Search 注入")
	}
	if opt.Think == nil {
		return nil, errors.New("guardian: 需要 Think 注入")
	}

	tried := map[string]bool{}
	seen := map[uint64]bool{}
	var orderQ []string
	var gathered []agent.Evidence
	q, ok, err := opt.Think(ctx, opt.Claim, orderQ)
	if err != nil {
		return nil, err
	}
	attempts := 0
	// 整循环：生成 query(先 Think) → 检索 → 判覆盖 → (缺才再 Think)
	for ok && attempts < opt.Budget {
		if tried[q] { // 防重复：Think 建议到已用 query 时视为无新角度
			nq, nok, nerr := opt.Think(ctx, opt.Claim, orderQ)
			if nerr != nil {
				return nil, nerr
			}
			if !nok || tried[nq] {
				break
			}
			q = nq
		}
		tried[q] = true
		orderQ = append(orderQ, q)
		attempts++

		hits, serr := opt.Search(ctx, opt.TenantID, q, opt.ScopeIDs...)
		if serr != nil {
			// 检索失败等同于"本 query 无收获"，不 halt（guardian 不因某一次抖动就把整稿打砸）。
			continue
		}
		for _, h := range hits {
			if seen[h.DocSentenceID] {
				continue
			}
			seen[h.DocSentenceID] = true
			gathered = append(gathered, h)
		}

		// 覆盖判定：已累计 >= MinCover 条可支撑证据 → accept
		if len(gathered) >= opt.MinCover {
			return &Decision{
				Verdict: VerdictAccept, Claim: opt.Claim,
				Reason:   fmt.Sprintf("已获得可支撑证据（检索 %d 次，命中 %d 条）", attempts, len(gathered)),
				Tried:    orderQ, Evidence: gathered,
			}, nil
		}

		// 尚未覆盖：换 query（排除已试）
		nq, nok, nerr := opt.Think(ctx, opt.Claim, orderQ)
		if nerr != nil {
			return nil, nerr
		}
		q, ok = nq, nok
	}

	// 预算尽仍不足以支撑：ask_human（把该补的都收敛成一条人话缺证）
	if len(gathered) == 0 {
		return &Decision{
			Verdict: VerdictAskHuman, Claim: opt.Claim,
			Reason: "知识库中没有来源能支撑该内容，需要你决定：补资料 / 降为定性 / 放弃该部分",
			Missing: []string{"该部分缺少知识库来源"},
			Tried:   orderQ,
		}, nil
	}
	return &Decision{ // 有少量命中但未达标也回 retry（不给 accept 也不硬抛），由外层决定是否 ask
		Verdict: VerdictRetry, Claim: opt.Claim,
		Reason:  fmt.Sprintf("命中不足(共 %d 条未达最低覆盖 %d)", len(gathered), opt.MinCover),
		Tried:   orderQ, Evidence: gathered,
	}, nil
}
