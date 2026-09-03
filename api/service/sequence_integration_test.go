//go:build integration

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestSequenceEdit_EndToEndWithCAS —— P08 change_list 在真 MySQL 上的端到端：
//   - 造 v1：三句 {A带证据5001待删, B带证据5002待改, C无据待留}；
//   - change_list: delete A / edit B(默认保绑定) / insert 无来源句到 B 后；
//     断言: v2 版本推进(ReferencedVersion=1)、顺序 [B改, 新句, C]、A 与其证据5001一并消失、
//     B 的5002证据经 edit 原样保留、insert 无源带 no_source 提醒、
//     旧 base(=1) 重复提交被 CAS 拒绝且版本不推进。
func TestSequenceEdit_EndToEndWithCAS(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99990050)
	w, _ := CreateWorkspace(ctx, tenantID, 1, "seq-e2e", nil)

	evidence := []agent.Evidence{
		{FileID: 11, DocSentenceID: 5001, VersionMd5: "v1", SourceText: "依据甲(A的)"},
		{FileID: 12, DocSentenceID: 5002, VersionMd5: "v1", SourceText: "依据乙(B的)"},
	}
	article := &agent.Article{
		Title: "稿",
		Sections: []agent.Section{{Paragraphs: []agent.Paragraph{{
			Sentences: []agent.Sentence{
				{Text: "句A将被删", EvidenceRefs: []uint64{0}},
				{Text: "句B将被改", EvidenceRefs: []uint64{1}},
				{Text: "句C无据保留", EvidenceRefs: []uint64{}},
			},
		}}}},
	}
	if _, err := PersistArticleSnapshot(ctx, tenantID, w.ID, article, evidence); err != nil {
		t.Fatalf("造 v1 失败: %v", err)
	}

	a, _ := storage.GetArticleByWorkspace(ctx, tenantID, w.ID)
	prev, _ := storage.GetLatestArticleVersion(ctx, a.ID)
	if prev.VersionNo != 1 {
		t.Fatalf("期望基线 v1, got %d", prev.VersionNo)
	}
	sents, _ := storage.ListArticleSentences(ctx, prev.ID)
	if len(sents) != 3 {
		t.Fatalf("v1 应有 3 句, got %d", len(sents))
	}
	idA, idB := sents[0].ID, sents[1].ID

	// 提交 change_list：删除 A(带证)、改 B(默认保绑定)、在 B 后插入一句无来源
	verID, reviews, serr := RunSequenceEdit(ctx, tenantID, 1, w.ID, &ChangeListRequest{
		Ops: []ChangeOp{
			{Op: "delete", TargetID: idA},
			{Op: "edit", TargetID: idB, NewText: "句B已被改"},
			{Op: "insert", AnchorID: idB, NewText: "新插句没文档支撑"},
		},
	})
	if serr != nil {
		t.Fatalf("序列编辑失败: %v", serr)
	}
	if verID == 0 {
		t.Fatal("未产新版本")
	}

	// 版本推进
	cur, _ := storage.GetLatestArticleVersion(ctx, a.ID)
	if cur.VersionNo != 2 || cur.ReferencedVersion != 1 {
		t.Errorf("期望 v2(ref v1), got v%d", cur.VersionNo)
	}
	after, _ := storage.ListArticleSentences(ctx, cur.ID)
	// 删除A + 插1 → 应为 3 行:[B改, 新句, C]
	if len(after) != 3 {
		t.Fatalf("删A插1(位置B后)应 3 句, got %d", len(after))
	}
	order := []string{after[0].Content, after[1].Content, after[2].Content}
	exp := []string{"句B已被改", "新插句没文档支撑", "句C无据保留"}
	for i := range exp {
		if order[i] != exp[i] {
			t.Errorf("顺序/内容不符: [%v], want [%v]", order, exp)
			break
		}
	}
	// A 与其证据 5001 一并消失；真源只剩 B 的 5002；同时 insert 无源句会落一条 no_source 占位(P09)。
	bindsNew, _ := storage.ListArticleBindings(ctx, cur.ID)
	realCount, nsCount := 0, 0
	for _, b := range bindsNew {
		if b.DocSentenceID != 0 {
			if b.DocSentenceID == 5002 {
				realCount++
			}
		} else if b.EvidenceStatus == "no_source" && b.SourceType == "knowledge" {
			nsCount++
		}
	}
	if realCount != 1 {
		t.Fatalf("真源应仅剩 B 的 5002 一条(被 edit 保留), real=%d", realCount)
	}
	if nsCount != 1 {
		t.Fatalf("insert 无源句应正好落 1 条 no_source 占位(黄点), got %d", nsCount)
	}
	// B 的证据挂在【edit 后的】句上
	b5002On := ""
	for _, b := range bindsNew {
		if b.DocSentenceID == 5002 {
			for _, s := range after {
				if s.ID == b.ArticleSentenceID {
					b5002On = s.Content
				}
			}
		}
	}
	if b5002On != "句B已被改" {
		t.Errorf("B 的证据未随 edit 后的句保留(target=%q)", b5002On)
	}

	// P09：找到"无来源插入句"并做一次人工取舍往返(no_source → ack_human 解除黄点 → reset 退回待核)。
	newRowID := uint64(0)
	for _, s := range after {
		if s.Content == "新插句没文档支撑" {
			newRowID = s.ID
		}
	}
	if newRowID == 0 {
		t.Fatal("未定位到插入句 id")
	}
	if err := MarkSentenceManual(ctx, tenantID, w.ID, newRowID, "ack_human"); err != nil {
		t.Fatalf("ack_human 失败: %v", err)
	}
	bindsAfter, _ := storage.ListArticleBindings(ctx, cur.ID)
	humanOn := false
	for _, b := range bindsAfter {
		if b.ArticleSentenceID == newRowID && b.EvidenceStatus == "human_kept" {
			humanOn = true
		}
	}
	if !humanOn {
		t.Errorf("ack_human 后应把该句占位从 no_source 置为 human_kept")
	}
	if err := MarkSentenceManual(ctx, tenantID, w.ID, newRowID, "reset_no_source"); err != nil {
		t.Fatalf("reset 回待核失败: %v", err)
	}
	// insert 无来源 → no_source 提醒且正文保留
	if len(reviews) == 0 {
		t.Error("insert 无来源应给 no_source 提醒")
	}

	// 并发/CAS：以过期 base(=1) 提交(已到 v2) → 拒绝且不推进版本
	verBefore := currentArticleVersion(ctx, tenantID, w.ID)
	_, _, cerr := RunSequenceEdit(ctx, tenantID, 1, w.ID, &ChangeListRequest{
		BaseArticleVersion: 1,
		Ops:                []ChangeOp{{Op: "edit", TargetID: sents[0].ID, NewText: "越权改"}},
	})
	if cerr == nil {
		t.Fatal("过期 base 应被拒绝")
	}
	if !errors.Is(cerr, ErrSequenceConflict) && !errors.Is(cerr, ErrArticleVersionConflict) {
		t.Errorf("期望 CAS 冲突错误, got %v", cerr)
	}
	if got := currentArticleVersion(ctx, tenantID, w.ID); got != verBefore {
		t.Errorf("被拒请求不应推进版本, before=%d got=%d", verBefore, got)
	}
}
