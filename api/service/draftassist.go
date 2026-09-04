package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/WWaynee/content-hub/agent/coordinator"
	"github.com/WWaynee/content-hub/splitter"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// draftassist.go — P10：拿来稿起稿（draft_assist）。
//
// 出处：packages/rebuild/P10、RFC rev-4 §13.5 / W5。
//
// 宏观上它解决的是什么（供答辩/评审，详见文末"回顾"小节的完整版）：
//   旧系统只有一条入口："新建工作区 → 填需求单 → 生成"。政企文案的真实高频开始方式是
//   "我手上已有一份类似稿子"（往期范文、上级模板、自己起草的半成品）——把这贴进来该走哪条路？
//   旧行为只会把它当"需求/素材"搜掉，或当空需求打断；"有稿要整理"与"从零要写"两套真实开始
//   方式被塞在一个表单里，后一种从未走通。
//
// 本文件给的落法：
//   - 需求单层显式立 source_kind：build_from_scratch（从零生成）/ draft_assist（贴稿起稿）；
//   - draft_assist 把用户草稿按"空行分段 + splitter.Sentences 完整句末断"切成句/段，
//     每句复用 P09 的 GovernManualSentence 治理（检索知识库 → bound / no_source / plausible），
//     一次 run(draft_assist) 落成 article_version v1：
//       bound    —— 草稿句在知识库配得上原文 → source_type=knowledge、evidence bound（可溯源）；
//       no_source—— 草稿里是"该有外部依据的断言"却拿不到 → 以 source_type=user_draft 的占位行保留，
//                    黄点待作者三选（保留/补料/删），绝不冒充有据、也不逼用户丢弃自己的话；
//       plausible—— 纯衔接/叙述（无数据断言）→ 按普通通稿句保留、不黄（P09 教训：别把没新事实
//                    的句子误标成"该有据却没据"）。
//   - 与从零生成共用同一套后续管线：产出的 v1 就是普通 article_version，后续 P08 受控编辑 /
//     P07 校验 / P04 溯源 / 导出全部天然可接。
//   - 治理是服务端真校验（复用 P09 链），不是让模型自报；治理链路连不上时保守落黄（fallback），
//     正文永远不丢、来源永远不冒充。

// 起稿来源常量（与 requirement.source_kind 列对齐）。
const (
	SourceKindFromScratch = "build_from_scratch" // 默认：填需求单 → 从零生成
	SourceKindDraftAssist = "draft_assist"       // 贴用户自带草稿 → split+治理 → 首版
)

var (
	// ErrDraftEmpty 没有可用的草稿正文（req.draft_input 与请求内文本均为空）。
	ErrDraftEmpty = errors.New("没有可用的草稿正文：请先粘贴你的草稿")
	// ErrDraftAssistOnlyOnce draft_assist 只在工作区尚无稿件版本时可执行（已有稿请走受控编辑/重生成）。
	ErrDraftAssistOnlyOnce = errors.New("该工作区已有稿件版本，不能重复从草稿起稿；请对稿件做受控修改或重新生成")
)

// draftGovernor 对一句草稿文本做治理的注入点（默认 GovernManualSentence；
// 测试注入 fake 以脱离 LLM/Qdrant 做确定性验证）。
type draftGovernor func(ctx context.Context, tenantID, workspaceID uint64, text string) (*GovernResult, error)

// draftGovernorFn 包级注入点：生产=GovernManualSentence；同包测试可替换。
var draftGovernorFn draftGovernor = GovernManualSentence

// parseDraftParagraphs 把草稿原文切成"段落"（空行分段）。
// 段落内多个非空行直接拼合（用户粘贴正文常含硬换行，不应把一个完整句拆成两行处理）；
// 明确 markdown 标题行（"# " / "## " 前缀）剥掉前缀并入文本（不做任何启发式猜标题，
// 见 GetArticle 注释——标题解析留给 P11，本层只负责稳定切句）。
// 返回非空段落列表（trim 后）。
func parseDraftParagraphs(text string) []string {
	var paras []string
	var buf []string
	flush := func() {
		if len(buf) == 0 {
			return
		}
		joined := strings.TrimSpace(strings.Join(buf, ""))
		if joined != "" {
			paras = append(paras, joined)
		}
		buf = buf[:0]
	}
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			flush()
			continue
		}
		t = strings.TrimPrefix(t, "## ")
		t = strings.TrimPrefix(t, "# ")
		buf = append(buf, t)
	}
	flush()
	return paras
}

// draftSentence 起稿过程中的一行句工作稿（落库前）。
type draftSentence struct {
	paraIndex int
	sentIndex int
	content   string
	binds     []model.EvidenceBinding // bound → knowledge/bound 行
	unsourced bool                    // 有断言却无外部可引 → 落 user_draft+no_source 占位（黄点）
}

// RunDraftAssist 执行一次"拿稿起稿"：
//  1. 取需求单草稿（overrideText 优先，其次 req.DraftInput），空 → ErrDraftEmpty；
//  2. 无稿件主记录时先建（current=0），已有版本号 >0 → ErrDraftAssistOnlyOnce（起稿只对空稿）；
//  3. 建 run(draft_assist)（同 ws active 排他）→ split(planner step) → 逐句治理(guardian step)；
//  4. CAS 0→1 落 v1：bound 绑定 knowledge/bound；no_source 落 source_type=user_draft 占位（黄点待作者三选）；
//     plausible 无绑定不黄；
//  5. 成功 FinishRunOk 并落 workspace=generated；失败 FailRun 并回退 workspace 状态。
//
// 返回 (article_version_id, reviews 人话提醒, error)。
func RunDraftAssist(ctx context.Context, tenantID, userID, workspaceID uint64, overrideText string) (uint64, []string, error) {
	req, err := storage.GetRequirementByWorkspace(ctx, tenantID, workspaceID)
	if err != nil {
		return 0, nil, errors.New("需求单不存在，无法从草稿起稿")
	}
	text := strings.TrimSpace(overrideText)
	if text == "" {
		text = strings.TrimSpace(req.DraftInput)
	}
	if text == "" {
		return 0, nil, ErrDraftEmpty
	}

	// 记录进入"生成中"前的工作区状态，供失败时回退
	var prevStatus string
	if ws, werr := storage.GetWorkspaceByID(ctx, tenantID, userID, workspaceID); werr == nil {
		prevStatus = ws.Status
	}
	_ = storage.UpdateWorkspaceStatus(ctx, workspaceID, "generating")

	// 1) 稿件主记录：起稿只对"尚无稿"的工作区（首次建 v1）；已有版本 → 拒绝（走受控编辑/重生成）
	a, aerr := storage.GetArticleByWorkspace(ctx, tenantID, workspaceID)
	if aerr != nil {
		a = &model.Article{WorkspaceID: workspaceID, TenantID: tenantID, Title: req.Title, Status: "generated"}
		if cerr := storage.CreateArticle(ctx, a); cerr != nil {
			restoreWSStatus(ctx, workspaceID, prevStatus)
			return 0, nil, fmt.Errorf("创建稿件记录失败: %w", cerr)
		}
	} else if a.CurrentVersionNo > 0 {
		restoreWSStatus(ctx, workspaceID, prevStatus)
		return 0, nil, ErrDraftAssistOnlyOnce
	}

	// 2) run 持久会话（active 排他：同一工作区不允许并行两次起稿/生产）
	co := coordinator.New()
	runRec, rerr := co.Start(ctx, coordinator.StartReq{
		TenantID: tenantID, UserID: userID, WorkspaceID: workspaceID,
		RunType: model.RunDraftAssist, BaseVersion: a.CurrentVersionNo, CurrentRole: "planner", Plan: text,
	})
	if rerr != nil {
		restoreWSStatus(ctx, workspaceID, prevStatus)
		if errors.Is(rerr, storage.ErrRunActive) {
			return 0, nil, ErrRunActive
		}
		return 0, nil, rerr
	}
	runID := runRec.ID

	failRun := func(reason string) {
		_ = storage.FailRun(ctx, runID, reason)
		restoreWSStatus(ctx, workspaceID, prevStatus)
	}

	// 3) 切分：段落（空行分隔）→ 段内完整句
	paras := parseDraftParagraphs(text)
	var sentences []draftSentence
	sentTotal := 0
	for pi, p := range paras {
		for si, s := range splitter.Sentences(p) {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			sentences = append(sentences, draftSentence{paraIndex: pi, sentIndex: si, content: s})
			sentTotal++
		}
	}
	if sentTotal == 0 {
		failRun("草稿切分后无任何完整句")
		return 0, nil, errors.New("草稿中未切出任何完整句（请检查是否只有标题/无正文标点）")
	}
	_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
		Role: model.RolePlanner, Action: "split_draft",
		Outcome:  model.OutcomeAccepted,
		Decision: fmt.Sprintf("把草稿按段落+完整句切为 %d 段 %d 句，准备逐句治理", len(paras), sentTotal),
	})

	// 4) 逐句治理（P09 真校验链：bound / no_source / plausible；服务不可达时保守落黄不夹断）
	var reviews []string
	for i := range sentences {
		s := &sentences[i]
		g, gerr := draftGovernorFn(ctx, tenantID, workspaceID, s.content)
		if gerr != nil {
			// 治理判不了：不丢用户文字，落 user_draft 黄点待作者复核
			s.unsourced = true
			reviews = append(reviews, fmt.Sprintf("“%s”治理失败(%s)，已按“用户草稿·待复核”保留。", truncateForReview(s.content), gerr.Error()))
			continue
		}
		switch g.ClaimType {
		case ClaimTypeBound:
			evs := make([]model.EvidenceBinding, 0, len(g.Sources))
			for j, src := range g.Sources {
				st := src.SourceType
				if st == "" {
					st = "knowledge"
				}
				evs = append(evs, model.EvidenceBinding{
					TenantID:       tenantID,
					SourceType:     st,
					DocFileID:      src.FileID,
					DocSentenceID:  src.DocSentenceID,
					EvidenceStatus: ClaimTypeBound,
					OrderNo:        j,
				})
			}
			s.binds = evs
		case ClaimTypeNoSource:
			// 草稿里有断言但知识库拿不到依据 → 用户草稿出身 + no_source（黄点，作者三选）
			s.unsourced = true
			reviews = append(reviews, "草稿句“"+truncateForReview(s.content)+"”含数据/事实断言，但知识库暂无可引来源，已按“用户草稿·无外部依据·待复核”保留。")
		default: // plausible：纯衔接/叙述，保留但不黄、也不伪称有据
			s.binds = nil
		}
		if g.Fallback {
			reviews = append(reviews, g.HumanText)
		}
	}
	_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
		Role: model.RoleGuardian, Action: "govern_draft_sentences",
		Successor: model.RoleWriter, Outcome: model.OutcomeAccepted,
		Decision: fmt.Sprintf("逐句治理 %d 句：bound=%d / no_source(用户草稿待核)=%d / plausible=%d",
			sentTotal, countClaim(sentences, "bound"), countUnsourced(sentences), sentTotal-countUnsourced(sentences)-countClaim(sentences, "bound")),
	})

	// 5) 乐观锁推进版本：0 → 1（唯一写者），并组 full_content（段落间空行，正文可读）
	base := uint64(a.CurrentVersionNo)
	next := base + 1
	ok, cErr := storage.CASBumpArticleCurrentVersion(ctx, a.ID, base, next)
	if cErr != nil {
		failRun(cErr.Error())
		return 0, reviews, fmt.Errorf("推进稿件版本失败: %w", cErr)
	}
	if !ok {
		failRun(ErrArticleVersionConflict.Error())
		return 0, reviews, ErrArticleVersionConflict
	}

	full := joinDraftFull(sentences)
	ver := &model.ArticleVersion{
		ArticleID:         a.ID,
		WorkspaceID:       workspaceID,
		TenantID:          tenantID,
		VersionNo:         int(next),
		FullContent:       full,
		Status:            "completed",
		ReferencedVersion: int(base),
	}

	// 6) 事务落库：version → sentences → bindings（bound）→ user_draft 占位（no_source）
	txErr := storage.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(ver).Error; err != nil {
			return err
		}
		rows := make([]model.ArticleSentence, 0, len(sentences))
		for _, w := range sentences {
			rows = append(rows, model.ArticleSentence{
				ArticleVersionID: ver.ID,
				WorkspaceID:      workspaceID,
				TenantID:         tenantID,
				SectionIndex:     0,
				ParagraphIndex:   w.paraIndex,
				SentenceIndex:    w.sentIndex,
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
			w := sentences[i]
			if len(w.binds) == 0 && w.unsourced {
				// P10：用户草稿断言无外部依据 → source_type=user_draft 占位（导出/展示可与 knowledge 区分）
				pb := model.EvidenceBinding{
					ArticleVersionID:  ver.ID,
					ArticleSentenceID: rows[i].ID,
					TenantID:          tenantID,
					SourceType:        "user_draft",
					EvidenceStatus:    ClaimTypeNoSource,
					OrderNo:           0,
				}
				if err := tx.Create(&pb).Error; err != nil {
					return err
				}
				continue
			}
			for _, b := range w.binds {
				b.ArticleVersionID = ver.ID
				b.ArticleSentenceID = rows[i].ID
				if err := tx.Create(&b).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if txErr != nil {
		failRun(txErr.Error())
		return 0, reviews, fmt.Errorf("草稿起稿快照落库失败: %w", txErr)
	}

	_, _ = storage.AppendStep(ctx, runID, model.AgentStep{
		Role: model.RoleWriter, Action: "draft_assist_snapshot",
		Outcome: model.OutcomeAccepted, RefID: ver.ID,
		Decision: fmt.Sprintf("按草稿治理结果落首版 article_version=%d（来源区分 knowledge/user_draft）", ver.ID),
	})
	if cerr := storage.FinishRunOk(ctx, runID, ver.ID); cerr != nil {
		return 0, reviews, cerr
	}
	_ = storage.UpdateWorkspaceStatus(ctx, workspaceID, "generated")
	return ver.ID, reviews, nil
}

// restoreWSStatus 失败时回退工作区状态（保持可操作，不卡死在 generating）。
func restoreWSStatus(ctx context.Context, wid uint64, prevStatus string) {
	if prevStatus == "" {
		prevStatus = "draft"
	}
	_ = storage.UpdateWorkspaceStatus(ctx, wid, prevStatus)
}

// joinDraftFull 按段落拼 full_content（段落间空行；段落内句子紧连，正文可直接 pre-wrap 阅读）。
func joinDraftFull(sents []draftSentence) string {
	var sb strings.Builder
	curPara := -1
	for _, s := range sents {
		if s.paraIndex != curPara {
			if curPara >= 0 {
				sb.WriteString("\n\n")
			}
			curPara = s.paraIndex
		}
		sb.WriteString(s.content)
	}
	return sb.String()
}

func countClaim(sents []draftSentence, claim string) int {
	n := 0
	for _, s := range sents {
		switch claim {
		case "bound":
			if len(s.binds) > 0 {
				n++
			}
		}
	}
	return n
}

func countUnsourced(sents []draftSentence) int {
	n := 0
	for _, s := range sents {
		if s.unsourced {
			n++
		}
	}
	return n
}
