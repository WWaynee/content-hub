//go:build integration

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// P10 draft_assist 真 MySQL 集成（治理注入 fake，脱离 LLM/Qdrant 做确定性验证）：
// 草稿 → 建 run(draft_assist) → 拆段句 → 逐句治理 → CAS 0→1 落 v1，
// 断言：bound 句落 knowledge/bound、断言无据句落 user_draft+no_source 占位（黄点）、
// 衔接句无绑定不黄；读侧 claim_type 顺序正确；run success 释放 active；
// 二次起稿被 ErrDraftAssistOnlyOnce 拒；导出区分 user_draft。

const draftAssistTestDraft = "欢迎广大考生报考我校。\n\n本次校园招聘共组织 40 场。\n\n我校2023年报名人数达1.2万人。"

func fakeDraftGovernor(_ context.Context, _ uint64, _ uint64, text string) (*GovernResult, error) {
	switch {
	case strings.Contains(text, "40 场"):
		return &GovernResult{Text: text, ClaimType: ClaimTypeBound,
			Sources:   []GovernSource{{FileID: 101, DocSentenceID: 9001, SourceType: "knowledge"}},
			HumanText: "已找到知识库来源"}, nil
	case strings.Contains(text, "报名人数"):
		return &GovernResult{Text: text, ClaimType: ClaimTypeNoSource,
			HumanText: "该句为数据断言但无库内依据，待人工取舍"}, nil
	default:
		return &GovernResult{Text: text, ClaimType: ClaimTypePlausibleAI,
			HumanText: "衔接/叙述语，无需外部依据"}, nil
	}
}

func TestDraftAssist_RunLifecycleAndUserDraft(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99990321)
	userID := uint64(42)

	orig := draftGovernorFn
	draftGovernorFn = fakeDraftGovernor
	defer func() { draftGovernorFn = orig }()

	w, werr := CreateWorkspace(ctx, tenantID, userID, "P10-draft-assist", &RequirementInput{
		Title:      "招生宣传稿",
		Platforms:  []string{"官网"},
		SourceKind: SourceKindDraftAssist,
		DraftInput: draftAssistTestDraft,
	})
	if werr != nil {
		t.Fatalf("create draft_assist workspace: %v", werr)
	}
	req, _ := storage.GetRequirementByWorkspace(ctx, tenantID, w.ID)
	if req.SourceKind != SourceKindDraftAssist || req.DraftInput == "" {
		t.Fatalf("需求单应带 source_kind=draft_assist 与草稿原文，实得 kind=%q", req.SourceKind)
	}

	verID, reviews, rerr := RunDraftAssist(ctx, tenantID, userID, w.ID, "")
	if rerr != nil {
		t.Fatalf("RunDraftAssist: %v", rerr)
	}
	if verID == 0 {
		t.Fatalf("应产出 article_version")
	}
	if len(reviews) == 0 {
		t.Fatalf("no_source 句应产生人话提醒")
	}

	// 1) 主记录 current_version_no=1
	a, aerr := storage.GetArticleByWorkspace(ctx, tenantID, w.ID)
	if aerr != nil || a.CurrentVersionNo != 1 {
		t.Fatalf("article 应推进到 v1，实得 current=%d err=%v", a.CurrentVersionNo, aerr)
	}

	// 2) v1 三句，来源绑定符合语义
	ver, _ := storage.GetLatestArticleVersion(ctx, a.ID)
	sents, _ := storage.ListArticleSentences(ctx, ver.ID)
	if len(sents) != 3 {
		t.Fatalf("应有 3 句，实得 %d", len(sents))
	}
	byContent := map[string]model.ArticleSentence{}
	for _, s := range sents {
		byContent[s.Content] = s
	}
	if _, ok := byContent["本次校园招聘共组织 40 场。"]; !ok {
		t.Fatalf("缺 bound 句：%v", sents)
	}
	if _, ok := byContent["我校2023年报名人数达1.2万人。"]; !ok {
		t.Fatalf("缺 no_source 句：%v", sents)
	}
	binds, _ := storage.ListArticleBindings(ctx, ver.ID)
	assertDraftBinds(t, byContent, binds)

	// 3) 读侧呈现。注意：bound 句的落库语义（source_type=knowledge/bound）已被上方 assertDraftBinds 锁定；
	// 读侧 LoadSentenceSources 对"孤儿绑定"（fake governor 给的 doc_id 9001 在本库并无真实原文行）
	// 会安全退化为无源(plausible-ai)——绝不把一条找不到原句的引用冒充成 bound，这是 P04 的防伪语义。
	// 真正把草稿句 bound 到可见 source 需真实知识库检索数据（P09 治理在生产链路中才做），
	// 此处集成重点是可确定性验证的：user_draft 占位读出 no_source 不被埋没/伪造。
	views := BuildSentenceViews(sents, LoadSentenceSources(ctx, tenantID, binds), ClaimStatusBySent(binds))
	if len(views) != 3 {
		t.Fatalf("应有 3 个 sentence_view，实得 %d", len(views))
	}
	if views[2].ClaimType != ClaimTypeNoSource {
		t.Fatalf("user_draft 占位句读出应=no_source，实得 %s（句：%s）", views[2].ClaimType, views[2].Text)
	}
	if views[1].ClaimType == ClaimTypeBound {
		t.Fatalf("孤儿绑定不应被误呈为 bound（句：%s）", views[1].Text)
	}
	// user_draft 占位不冒充满源：确认该句不在真 sources 中出现
	for _, v := range views {
		if v.ClaimType == ClaimTypeBound {
			t.Fatalf("本测试无真实 KBase 数据，不应出现任何 bound 呈现：%s", v.Text)
		}
	}

	// 4) run 成功释放 active
	runs, _ := storage.ListRunsByWorkspace(ctx, tenantID, w.ID)
	if len(runs) != 1 || runs[0].RunType != string(model.RunDraftAssist) {
		t.Fatalf("应恰好 1 条 draft_assist run，实得 %+v", runs)
	}
	if runs[0].Status != string(model.RunSuccess) || runs[0].Active {
		t.Fatalf("run 应 success+非 active，实得 %s active=%v", runs[0].Status, runs[0].Active)
	}

	// 5) 工作区状态落 generated
	ws, _ := storage.GetWorkspaceByID(ctx, tenantID, userID, w.ID)
	if ws.Status != "generated" {
		t.Fatalf("workspace 应落 generated，实得 %s", ws.Status)
	}

	// 6) 二次起稿拒绝（已有版本，走受控编辑）
	if _, _, rerr2 := RunDraftAssist(ctx, tenantID, userID, w.ID, ""); rerr2 != ErrDraftAssistOnlyOnce {
		t.Fatalf("二次起稿应被 ErrDraftAssistOnlyOnce 拒，实得 %v", rerr2)
	}

	// 7) 导出区分 user_draft（清单含"来源：用户草稿"，不含伪造文档来源）
	md, xerr := ExportArticle(ctx, ver.ID)
	if xerr != nil {
		t.Fatalf("export: %v", xerr)
	}
	if !strings.Contains(md, "用户草稿") || !strings.Contains(md, "报名人数") {
		t.Fatalf("导出应标注 user_draft 句，实得：\n%s", md)
	}
	if strings.Contains(md, "来源文档：") {
		// fake 的 doc 9001 在库中不存在 → 孤儿绑定安全略过，不应出现来源文档行
		t.Fatalf("孤儿绑定不应出现在导出清单：\n%s", md)
	}
}

// assertDraftBinds 校验三句的来源落库：bound→knowledge/bound；断言无据→user_draft+no_source；衔接→无绑定。
func assertDraftBinds(t *testing.T, byContent map[string]model.ArticleSentence, binds []model.EvidenceBinding) {
	t.Helper()
	boundSent := byContent["本次校园招聘共组织 40 场。"]
	noSrcSent := byContent["我校2023年报名人数达1.2万人。"]
	plainSent := byContent["欢迎广大考生报考我校。"]
	bySentID := map[uint64][]model.EvidenceBinding{}
	for _, b := range binds {
		bySentID[b.ArticleSentenceID] = append(bySentID[b.ArticleSentenceID], b)
	}
	got := map[string][]string{}
	for content, s := range byContent {
		got[content] = []string{}
		for _, b := range bySentID[s.ID] {
			got[content] = append(got[content], b.SourceType+"/"+b.EvidenceStatus)
		}
	}
	want := map[string][]string{
		boundSent.Content: {"knowledge/bound"},
		noSrcSent.Content: {"user_draft/no_source"},
		plainSent.Content: {},
	}
	for content, w := range want {
		g, ok := got[content]
		if !ok {
			t.Fatalf("句 %q 应存在绑定", content)
		}
		if len(g) != len(w) {
			t.Fatalf("句 %q 绑定数应=%d(%v) 实得=%v", content, len(w), w, g)
		}
		for i := range w {
			if g[i] != w[i] {
				t.Fatalf("句 %q 绑定应=%v 实得=%v", content, w, g)
			}
		}
	}
	_ = boundSent
}
