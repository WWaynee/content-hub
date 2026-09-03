package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// sequence.go — P08：稳定句身份 + change_list 序列编辑(edit/insert/delete/move)的可执行地基。
//
// 出处：RFC rev-4 §13.3 / W3、packages/rebuild/P08。
//
// 宏观上讲它解决的是什么（供答辩/评审，详见文末"回顾"小节的完整版）：
//   旧实现里"我要动哪一句"被表达成"整篇扁平 list 的下标整数"；system 没有 delete/move/前插能力。
//   一旦用户真想在稿子上删一句、把某句挪到别处、在某句前插一句，后端没有一个能承接"删/移/插"的模型：
//   - 所有按 index 的定位（AI 续改、证据继承、tooltip 引用）都踩"下标漂移"——删了第 3 句，原第 4~N 句全错位；
//   - 证据绑定还是拿着"被删除句"的旧文继承，切不出对错；
//   等于用户一旦不再"线性整篇/末尾追加/改第 i 句"，就没有写稿的通路。
//
// 本文件给的落法（与实现方/评审对齐后的权衡）：
//   - 稳定锚用句子的 PK(article_sentence_id)，不再用"第 i 句"整数；
//   - 顺序仍由现有 (section_index, paragraph_index, sentence_index) 三元承载，增/删/移后由执行器就地重排
//     （同一段内句号 0..n 重编），不新增独立的全局序列列——避免与既有表/读侧那个"孤儿序字段"双写。
//   - 一次受控编辑载体 = change_list[] { edit | insert | delete | move } + base_article_version，
//     搭配 P02 乐观锁(CAS) 一次形成一个新的 article_version（快照式），未动句证据随句走。
//   - 本执行器是纯序列层：不内嵌 LLM/检索。要为新句附证据，由调用方把 evidence 载荷放进 op；
//     不带则那句本层落为"无 external source"（读侧 claim_type=plausible-ai，语义该不该有据由 P09 贴 no_source）。
//     这兑现 rev-3『别让系统把没据的东西偷偷编成有据』。
//
// 闭包不变式（rev-3 §12.5 / W3，单测据此断言）——未授权句绝不被改：
//   - edit      ：只改那一句文本（段落归属不动），证据默认沿旧（防止"只改措辞却被误删来源"）；
//                  若显式 ClearEvidence.re 或给新 Evidence 则重写该句证据。
//   - delete    ：该句及其绑定被摘除，新版本里不再有它，也不留悬空引用给读取。
//   - insert    ：新句进 anchor 段落、紧随其后；无证据载荷 → no_source 提醒(见返回 reviews)，正文不回退。
//   - move      ：仅调位置，文本与绑定原样跟随（move ≠ 重写）。
//   - 除上述被展示为新增/删除的句外，其余句子的 srcID 集合在输入与输出之间差集为空（closed set）。

// 错误（供上层 handler 做向用户可读的 error wrap）
var (
	// ErrSequenceConflict 提交的 base_article_version 与稿件当前版本号不一致（被 CAS 拒绝的信号）。
	ErrSequenceConflict = errors.New("稿件已被更新，请基于最新版本重新提交")
	errOpUnknown        = errors.New("SequenceOp 未知")
	errNeedTarget       = errors.New("该 op 需要 target 句定位")
	errNeedAnchor       = errors.New("该 op 需要 anchor 句定位插入/移动位置")
	errEmptyText        = errors.New("句子文本不能为空")
)

// ---------- 对外请求契约 ----------

// ChangeEvidence 序列 op 显式携带的一条证据载荷（供 edit 重建 / insert 新句绑定；由上层检索给出）。
type ChangeEvidence struct {
	FileID        uint64 `json:"file_id"`
	DocSentenceID uint64 `json:"doc_sentence_id"`
	SourceType    string `json:"source_type,omitempty"` // 缺省 = knowledge（P09 user_draft 等由上层填）
}

// ChangeOp 一个原子序列操作。
type ChangeOp struct {
	// Op edit | insert | delete | move
	Op string `json:"op"`
	// TargetID edit/delete 的目标句、move 的源句（均为 article_sentence_id 稳定锚）。
	TargetID uint64 `json:"target_sentence_id,omitempty"`
	// AnchorID insert/move 的落地锚：把新句/被移句放到该句之后（同段内）。
	AnchorID uint64 `json:"anchor_sentence_id,omitempty"`
	// NewText edit 的新文本 / insert 的新句子文本。
	NewText string `json:"new_text,omitempty"`
	// Evidence edit 重建证据 或 insert 新句携带的证据载荷（可选）。
	Evidence []ChangeEvidence `json:"evidence,omitempty"`
	// ClearEvidence（仅编辑用）为 true 且未给 Evidence → 显式清空该句证据（改文后原来源不再可信）。
	ClearEvidence bool `json:"clear_evidence,omitempty"`
}

// ChangeListRequest 一次受控序列编辑的完整请求（change_list）。
type ChangeListRequest struct {
	BaseArticleVersion int        `json:"base_article_version"`
	Ops                []ChangeOp `json:"ops"`
}

// ---------- 内部工作稿 ----------

// seqSentence 内部一行句草稿。
// srcID 记录"它在哪个版本长出来"的源 PK（取自源版本）；新增(insert)的 srcID = 0。
// 落库后新版本会给真正的新 PK，srcID 仅用来做 closed-set 断言，不与落库后的业务 id 混淆。
type seqSentence struct {
	srcID   uint64 // 0 = 本次 insert 新增
	content string
	sec     int
	para    int
	sent    int
	binds   []model.EvidenceBinding
	// P09：本句属于“新增进来 / 被明确去掉来源、但仍找不到任何外部可引”，需要在落库时给
	// 一条 evidence_status='no_source' 的占位行供 UI 显示黄点（而不是闷声当 plausible 放行）。
	unsourced bool
}

// seqPlan 执行结果：新的有序句草稿 + 供 run step/返回的人话级提醒。
type seqPlan struct {
	sents   []seqSentence
	reviews []string // 无来源提醒等；正文不会因此回退
}

// ---------- 纯执行（无 DB / 无 LLM，可单测） ----------

// buildEntryBinds 把一个 op 携带的证据载荷转成 model binding 草稿。
func buildEntryBinds(evs []ChangeEvidence, tenantID uint64) []model.EvidenceBinding {
	if len(evs) == 0 {
		return nil
	}
	out := make([]model.EvidenceBinding, 0, len(evs))
	for i, e := range evs {
		st := e.SourceType
		if st == "" {
			st = "knowledge"
		}
		out = append(out, model.EvidenceBinding{
			TenantID:       tenantID,
			SourceType:     st,
			DocFileID:      e.FileID,
			DocSentenceID:  e.DocSentenceID,
			EvidenceStatus: "bound",
			OrderNo:        i,
		})
	}
	return out
}

// cloneBinds 拷贝一段绑定为一个"将由落库回填引用的新草稿"（清掉旧引用，只保留来源语义）。
func cloneBinds(src []model.EvidenceBinding) []model.EvidenceBinding {
	if len(src) == 0 {
		return nil
	}
	out := make([]model.EvidenceBinding, 0, len(src))
	for _, b := range src {
		cp := b
		cp.ID = 0
		cp.ArticleVersionID = 0
		cp.ArticleSentenceID = 0
		out = append(out, cp)
	}
	return out
}

func applyChangePlan(src []model.ArticleSentence, bindBySent map[uint64][]model.EvidenceBinding, req *ChangeListRequest, tenantID uint64) (*seqPlan, error) {
	// 工作稿拷贝 + 首张 source→位置索引
	wise := make([]seqSentence, len(src))
	for i, s := range src {
		wise[i] = seqSentence{
			srcID:   s.ID,
			content: s.Content,
			sec:     s.SectionIndex,
			para:    s.ParagraphIndex,
			sent:    s.SentenceIndex,
			binds:   cloneBinds(bindBySent[s.ID]),
		}
	}
	idxByID := func() map[uint64]int {
		m := make(map[uint64]int, len(wise))
		for i, w := range wise {
			if w.srcID == 0 { // 新增句没有可输入的稳定锚，跳过
				continue
			}
			m[w.srcID] = i
		}
		return m
	}

	var reviews []string
	idx := idxByID()

	for _, op := range req.Ops {
		switch op.Op {
		case "edit":
			i, ok := idx[op.TargetID]
			if !ok {
				return nil, fmt.Errorf("%w (target_sentence_id=%d)", errNeedTarget, op.TargetID)
			}
			t := strings.TrimSpace(op.NewText)
			if t == "" {
				return nil, errEmptyText
			}
			wd := wise[i]
			wd.content = t // 保留段落归属不动
			// 证据策略：显式清空 > 给 Evidence 重建 > 默认保留原绑定
			switch {
			case op.ClearEvidence && len(op.Evidence) == 0:
				// P09：明确去掉来源但无替代可引 → 文案再不可装成"有据"，标黄待核。
				wd.binds = nil
				wd.unsourced = true
				reviews = append(reviews, fmt.Sprintf("已去掉“%s”原来源，但暂无可引用资料替换，正文按无外部依据(no_source)保留待核", truncateForReview(t)))
			case len(op.Evidence) > 0:
				wd.binds = buildEntryBinds(op.Evidence, tenantID)
				wd.unsourced = false
			default: // 默认保留：改措辞不卸下来源
				wd.unsourced = false
			}
			wise[i] = wd

		case "insert":
			if op.AnchorID == 0 {
				return nil, fmt.Errorf("%w (insert)", errNeedAnchor)
			}
			ai, ok := idx[op.AnchorID]
			if !ok {
				return nil, fmt.Errorf("insert：anchor_sentence_id=%d 在当前稿中不存在", op.AnchorID)
			}
			t := strings.TrimSpace(op.NewText)
			if t == "" {
				return nil, errEmptyText
			}
			an := wise[ai]
			ins := seqSentence{
				srcID: 0, content: t,
				sec: an.sec, para: an.para,
				binds: buildEntryBinds(op.Evidence, tenantID),
			}
			// P09：带不出可引证据的新句 → 落 no_source 占位供黄点，正文不回退、不闷声放行。
			if len(ins.binds) == 0 {
				ins.unsourced = true
				reviews = append(reviews, fmt.Sprintf("新增句“%s”暂无外部来源，请核对是否需补依据(no_source 待核)", truncateForReview(t)))
			}
			// 把新句放到 anchor 之后
			wise = append(wise[:ai+1], append([]seqSentence{ins}, wise[ai+1:]...)...)
			idx = idxByID()

		case "delete":
			i, ok := idx[op.TargetID]
			if !ok {
				return nil, fmt.Errorf("%w (target_sentence_id=%d)", errNeedTarget, op.TargetID)
			}
			// 摘除该行：绑定随行消失（不留悬空）
			wise = append(wise[:i], wise[i+1:]...)
			idx = idxByID()

		case "move":
			if op.AnchorID == 0 {
				return nil, fmt.Errorf("%w (move)", errNeedAnchor)
			}
			if op.TargetID == op.AnchorID {
				return nil, errors.New("move：不能把句子挪到自己后面")
			}
			ti, ok := idx[op.TargetID]
			if !ok {
				return nil, fmt.Errorf("move：target_sentence_id=%d 不存在", op.TargetID)
			}
			// 摘出目标 (文本+绑定原样跟随）
			moved := wise[ti]
			wise = append(wise[:ti], wise[ti+1:]...)
			idx = idxByID()
			ai, ok := idx[op.AnchorID] // 摘除后锚下标可能变化实时定位
			if !ok {
				return nil, fmt.Errorf("move：anchor_sentence_id=%d 不存在", op.AnchorID)
			}
			an := wise[ai]
			moved.sec = an.sec
			moved.para = an.para
			wise = append(wise[:ai+1], append([]seqSentence{moved}, wise[ai+1:]...)...)
			idx = idxByID()

		default:
			return nil, fmt.Errorf("%w: %q", errOpUnknown, op.Op)
		}
	}

	// 统一按 (sec,para) 连续段重编 sent 0..n，消灭"同段同 sent 号"歧义并保证三元升序可读。
	wise = reflowSents(wise)
	return &seqPlan{sents: wise, reviews: reviews}, nil
}

// reflowSents 对每段（连续相同 sec/para 的 run）内句号就地 0..n 重排；段间跨越用 para 变化分界。
func reflowSents(wise []seqSentence) []seqSentence {
	out := make([]seqSentence, len(wise))
	if len(wise) == 0 {
		return out
	}
	start := 0
	flush := func(end int) {
		sent := 0
		for k := start; k < end; k++ {
			w := wise[k]
			w.sent = sent
			sent++
			out[k] = w
		}
	}
	for i := 1; i < len(wise); i++ {
		prev, cur := wise[i-1], wise[i]
		if prev.sec != cur.sec || prev.para != cur.para {
			flush(i)
			start = i
		}
	}
	flush(len(wise))
	return out
}

func truncateForReview(s string) string {
	r := []rune(s)
	if len(r) > 24 {
		return string(r[:24]) + "…"
	}
	return s
}

// ---------- DB 侧落库 ----------

// applySequenceVersion 在给定上下文落一部待写新版本（内部事务,复用现有 CAS/事务语义）。
// 返回 (versionID, reviews, error)；reviews 为该批 seq 产生的对人话级提示（如 no_source 待核）。
//
// 仅在 base 与稿件当前版本匹配时 CAS 推进一次；失败则返回 ErrSequenceConflict，不产生新版本。
func applySequenceVersion(ctx context.Context, tenantID, workspaceID uint64, req *ChangeListRequest) (uint64, []string, error) {
	if req == nil || len(req.Ops) == 0 {
		return 0, nil, errors.New("change_list 无任何操作")
	}
	a, err := storage.GetArticleByWorkspace(ctx, tenantID, workspaceID)
	if err != nil {
		return 0, nil, fmt.Errorf("稿件不存在")
	}
	// 必须存在一份可被编辑的最新版本（sequence 不能凭空对"无稿"做）
	prev, err := storage.GetLatestArticleVersion(ctx, a.ID)
	if err != nil {
		return 0, nil, fmt.Errorf("尚无稿件（没有可编辑的最新版本）：%w", err)
	}
	sents, err := storage.ListArticleSentences(ctx, prev.ID)
	if err != nil {
		return 0, nil, err
	}
	// 传入 base 版本号：客户端可显式带(做 CAS 前瞻校验);缺省(==0)则以稿件当前版本号为基准。
	if req.BaseArticleVersion != 0 && req.BaseArticleVersion != a.CurrentVersionNo {
		return 0, nil, ErrSequenceConflict
	}
	validBase := req.BaseArticleVersion
	if validBase == 0 {
		validBase = a.CurrentVersionNo
		req.BaseArticleVersion = validBase
	}
	base := uint64(validBase)
	next := base + 1
	// 闭包输入：读取绑定按句聚好
	bindsAll, err := storage.ListArticleBindings(ctx, prev.ID)
	if err != nil {
		return 0, nil, err
	}
	bindBySent := map[uint64][]model.EvidenceBinding{}
	for _, b := range bindsAll {
		if b.ArticleSentenceID == 0 {
			continue
		}
		bindBySent[b.ArticleSentenceID] = append(bindBySent[b.ArticleSentenceID], b)
	}

	// 纯执行（含所有 closed-set 不变式，出错即中止，不落库）
	plan, err := applyChangePlan(sents, bindBySent, req, tenantID)
	if err != nil {
		return 0, nil, err
	}

	// 落库前无句（理论上 change plan 至少应留或被真删空——空稿也算一种结果，仍允许生成空版本）
	// 乐观锁：只有把 current_version_no: base → base+1 拿到真写权的继续落，另一个并发提交会 conflict。
	ok, cErr := storage.CASBumpArticleCurrentVersion(ctx, a.ID, base, next)
	if cErr != nil {
		return 0, nil, fmt.Errorf("推进稿件版本失败: %w", cErr)
	}
	if !ok {
		return 0, nil, ErrSequenceConflict
	}

	// 组 full_content（线性拼接）与新绑定的工作稿
	var sb strings.Builder
	for _, w := range plan.sents {
		sb.WriteString(w.content)
	}
	ver := &model.ArticleVersion{
		ArticleID:         a.ID,
		WorkspaceID:       workspaceID,
		TenantID:          tenantID,
		VersionNo:         int(next),
		FullContent:       sb.String(),
		Status:            "completed",
		ReferencedVersion: int(base),
	}

	// 事务落：version -> sentences(取新 ID) -> bindings(回填 ArticleSentenceID)
	err = storage.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(ver).Error; err != nil {
			return err
		}
		rows := make([]model.ArticleSentence, 0, len(plan.sents))
		for _, w := range plan.sents {
			rows = append(rows, model.ArticleSentence{
				ArticleVersionID: ver.ID,
				WorkspaceID:      workspaceID,
				TenantID:         tenantID,
				SectionIndex:     w.sec,
				ParagraphIndex:   w.para,
				SentenceIndex:    w.sent,
				Content:          w.content,
			})
		}
		if len(rows) == 0 {
			return nil
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			s := plan.sents[i]
			if len(s.binds) == 0 && s.unsourced {
				// P09：无外部可引句子 → 落占位状态行(no_source)。读侧以此显示黄点与三选项，不与真"bound"混淆。
				pb := model.EvidenceBinding{
					ArticleVersionID:  ver.ID,
					ArticleSentenceID: rows[i].ID,
					TenantID:          tenantID,
					SourceType:        "knowledge",
					EvidenceStatus:    "no_source",
					OrderNo:           0,
				}
				if err := tx.Create(&pb).Error; err != nil {
					return err
				}
				continue
			}
			for _, b := range plan.sents[i].binds {
				b.ArticleVersionID = ver.ID
				b.ArticleSentenceID = rows[i].ID
				if err := tx.Create(&b).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, nil, fmt.Errorf("落序列编辑快照失败: %w", err)
	}
	return ver.ID, plan.reviews, nil
}
