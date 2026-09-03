// Package verifier 提供"确定性规则优先、LLM 近义兜底"的事实断言检真(P07/RFC rev-1 §3.1)。
//
// 核心主张：数值/日期/百分比是否在证据原文里,不应由 LLM 自报 bool 决定(factverifier 曾直接采信),
// 而应先用确定性的归一/包含/词覆盖判定；规则判不了的纯语义近义才降级 LLM,且必须带可引证据下标——
// "凡 supported 必须给出可引证据",否则不允许写 bound。统计求和/派申一律不判 supported。
//
// 本包是纯工具(deterministic),不依赖 service/llm 判定结果,便于离线反例集单测。
package verifier

import (
	"regexp"
	"strconv"
	"strings"
)

// Verdict 一次断言支撑判定。
type Verdict string

const (
	// Cover: 确定性规则命中某条证据原文 → supported(给 EvidenceIdx/int)
	// UnsupportedRule: 规则明确判非(候选证据无匹配且断言属"导出/求和/以他人证据替") → not supported
	// LowConf: 规则无法定论(疑纯语义近义/词形差异),交给 LLM 近义兜底
	Cover           Verdict = "supported"
	UnsupportedRule Verdict = "unsupported"
	LowConf         Verdict = "low_confidence"
)

// Result 一条断言判定。
type Result struct {
	Verdict Verdict
	// EvidenceIdx 命中的证据下标(仅 Cover 有意义)。
	EvidenceIdx int
	Reason      string
}

// Rule 纯规则判定对象。
type Rule struct{}

// Decide 对单条断言 text 在所有候选证据原文上做规则判定(evidences 下标=证据在数组中的位置)。
func (Rule) Decide(assertion string, evidences []string) Result {
	nums, hasPct := extractNums(assertion)

	// 规则 0：断言整体(去空白紧凑)被某条证据原文包含 → 直接 supported。
	compact := compactCW(assertion)
	for i, e := range evidences {
		if compact != "" && strings.Contains(compactCW(e), compact) {
			return Result{Verdict: Cover, EvidenceIdx: i, Reason: "断言原文被证据原文包含"}
		}
	}

	// 强类型数值/百分比断言：归一后单条证据全覆盖才 supported。
	if len(nums) > 0 {
		best := findSingleCovering(nums, evidences)
		switch best {
		case -2: // 两条及以上证据各自全含 → 歧义,不能标单一来源(防各取一半拼证) → LowConf
			return Result{Verdict: LowConf, EvidenceIdx: -1, Reason: "多条证据都可单独覆盖,需澄清来源,交 LLM/人工"}
		case -1:
			if hasStatisticalWord(assertion) || hasPct {
				// 查不到 + 含统计词(或百分比是明显的"占比结论")：宁可 unsupported,禁止由明细自行算出
				if hasStatisticalWord(assertion) {
					return Result{Verdict: UnsupportedRule, EvidenceIdx: -1,
						Reason: "含统计/推导词且数值不在原文,禁止明细求和或推算"}
				}
			}
			return Result{Verdict: LowConf, EvidenceIdx: -1,
				Reason: "存在未能在证据原文直接找到的数值/占比,交 LLM 判断是否纯语义同义"}
		default: // >=0
			return Result{Verdict: Cover, EvidenceIdx: best, Reason: "断言数值在单条证据原文中归一命中"}
		}
	}

	// 一般事实断言(无独立数值 token)：符号包含未中,规则无法判近义 → LowConf(交给 LLM 近义)。
	return Result{Verdict: LowConf, EvidenceIdx: -1,
		Reason: "一般事实断言无法规则判定(除原文直接包含),交 LLM 低置信核实"}
}

// compactCW 去空与全角空格,便于中文紧凑比较。
func compactCW(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// extractNums 抽取出文本中的独立数值 token(整数/小数),并返回是否含百分比字样。
func extractNums(a string) ([]string, bool) {
	re := regexp.MustCompile(`(\d+(?:\.\d+)?)`)
	m := re.FindAllString(a, -1)
	return m, strings.ContainsAny(a, "%％占比")
}

// 统计推导词表。
func hasStatisticalWord(a string) bool {
	for _, w := range []string{"合计", "总计", "累计", "总共", "据此", "推得", "除以", "相加", "总场次",
		"工作日", "由此推算", "合计达", "合共"} {
		if strings.Contains(a, w) {
			return true
		}
	}
	return false
}

// findSingleCovering: 找到“唯一一条覆盖了断言全部数值 token”的证据下标。
// 返回：>=0 唯一者下标；(不存在)-1；(>1 条都能全覆盖，歧义)-2。
func findSingleCovering(canon []string, evidences []string) int {
	numRE := regexp.MustCompile(`\d+(?:\.\d+)?`)
	hits := make([]int, 0)
	for i, e := range evidences {
		ev := numRE.FindAllString(e, -1)
		if len(ev) == 0 {
			continue
		}
		need := make(map[string]bool, len(canon))
		for _, c := range canon {
			need[c] = true
		}
		for _, v := range ev {
			for c := range need {
				if eqNumeric(c, v) {
					delete(need, c)
				}
			}
		}
		if len(need) == 0 {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		return -1
	case 1:
		return hits[0]
	default:
		return -2
	}
}

// eqNumeric 数值等价(int/float 归一后比较)。
func eqNumeric(a, b string) bool {
	af, ae := strconv.ParseFloat(a, 64)
	bf, be := strconv.ParseFloat(b, 64)
	if ae == nil && be == nil {
		return af == bf
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}
