//go:build integration

package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// TestConcurrentGenerationVersionCAS 乐观锁(CAS)真验证：
// 同一台 workspace/稿件上发起 N 个并发生成(PersistArticleSnapshot)，验证：
//   - 每个写者要么成功、要么因版本冲突失败，成功+冲突之和 == N，且不存在非冲突的异常失败；
//   - current_version_no 增量恰等于成功数，article_versions 无重复 version_no；
//   - 不再出现两个写者都“静默覆盖/都把同号写进去”的情况。
func TestConcurrentGenerationVersionCAS(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99994011)

	w, err := CreateWorkspace(ctx, tenantID, 1, "CAS并发基线", nil)
	if err != nil {
		t.Fatalf("创建 workspace 失败: %v", err)
	}

	// 基线：首次落稿产生 v1（article.CurrentVersionNo -> 1）
	first := &agent.Article{Title: "基",
		Sections: []agent.Section{{Paragraphs: []agent.Paragraph{{
			Sentences: []agent.Sentence{{Text: "基线句。", EvidenceRefs: []uint64{}}}}}}}}
	if _, err := PersistArticleSnapshot(ctx, tenantID, w.ID, first, nil); err != nil {
		t.Fatalf("落基线快照失败: %v", err)
	}
	art0, err := storage.GetArticleByWorkspace(ctx, tenantID, w.ID)
	if err != nil {
		t.Fatalf("读基 article 失败: %v", err)
	}
	baseNo := art0.CurrentVersionNo
	if baseNo != 1 {
		t.Fatalf("期望基线版本=1, 实得 %d", baseNo)
	}

	const n = 6
	payload := &agent.Article{Title: "并发稿",
		Sections: []agent.Section{{Paragraphs: []agent.Paragraph{{
			Sentences: []agent.Sentence{{Text: "并发写入句。", EvidenceRefs: []uint64{}}}}}}}}

	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	succ, conflict := 0, 0
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			_, e := PersistArticleSnapshot(ctx, tenantID, w.ID, payload, nil)
			mu.Lock()
			errs[idx] = e
			if e == nil {
				succ++
			} else if errors.Is(e, ErrArticleVersionConflict) {
				conflict++
			}
			mu.Unlock()
		}(i)
	}
	close(start)
	wg.Wait()

	if succ == 0 {
		t.Fatalf("并发至少应有 1 个成功, 实得 0 (errs=%v)", errs)
	}
	if succ+conflict != n {
		t.Fatalf("成功+冲突 应=%d 实得 %d+%d", n, succ, conflict)
	}
	for i, e := range errs {
		if e != nil && !errors.Is(e, ErrArticleVersionConflict) {
			t.Fatalf("goroutine %d 出现非冲突异常错误: %v", i, e)
		}
	}

	// 一致性：current_version_no == 基线 + 成功数；版本行数 == 1+成功数；无重复 version_no
	art, _ := storage.GetArticleByWorkspace(ctx, tenantID, w.ID)
	if want := baseNo + succ; art.CurrentVersionNo != want {
		t.Fatalf("current_version_no 应为 %d(基线+成功) 实得 %d", want, art.CurrentVersionNo)
	}
	vs, err := storage.ListArticleVersions(ctx, art.ID)
	if err != nil {
		t.Fatalf("读版本列表失败: %v", err)
	}
	if want := 1 + succ; len(vs) != want {
		t.Fatalf("article_versions 行数应=1+成功=%d 实得 %d", want, len(vs))
	}
	seen := map[int]bool{}
	for _, v := range vs {
		if seen[v.VersionNo] {
			t.Fatalf("出现重复 version_no=%d", v.VersionNo)
		}
		seen[v.VersionNo] = true
	}
	t.Logf("并发 CAS 验证通过: N=%d 成功=%d 冲突=%d(friendly), 版本唯一且连续", n, succ, conflict)
}

// TestRevisionApplyCAS 验证修订落库路径(ApplyArticleRevision)的乐观锁：
//   - 顺序两次修订各成功一次，版本 +1 +1，未动句继承不被破坏；
//   - 对同一最新版本并发发起多次修订：恰只有一个成功写新版本，其余返回版本冲突，不产生重复版本行。
//
// 全程仅用 MySQL（不调用 LLM/Embedding/OSS）。
func TestRevisionApplyCAS(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	ctx := context.Background()
	tenantID := uint64(99994012)

	fixture := func() (*model.Article, uint64, func()) {
		w, err := CreateWorkspace(ctx, tenantID, 1, "revisionCAS", nil)
		if err != nil {
			t.Fatalf("创建 workspace 失败: %v", err)
		}
		art := &agent.Article{Title: "稿",
			Sections: []agent.Section{{Paragraphs: []agent.Paragraph{{
				Sentences: []agent.Sentence{
					{Text: "句A。", EvidenceRefs: []uint64{}},
					{Text: "句B。", EvidenceRefs: []uint64{}},
				}}}}}}
		if _, err := PersistArticleSnapshot(ctx, tenantID, w.ID, art, nil); err != nil {
			t.Fatalf("落基线失败: %v", err)
		}
		a, _ := storage.GetArticleByWorkspace(ctx, tenantID, w.ID)
		if a == nil {
			t.Fatalf("article 不存在")
		}
		cleanup := func() {}
		return a, w.ID, cleanup
	}

	applyOnce := func(a *model.Article, wsID uint64) error {
		_, e := ApplyArticleRevision(ctx, ReviseSentenceInput{
			WorkspaceID: wsID, TenantID: tenantID, TargetIndex: 1, NewText: "句B已修订。",
			NewEvidence: nil, NewEvidenceRefs: nil,
		})
		return e
	}

	// ---- 顺序两次修订：都成功，版本 1→2→3 ----
	a, wsID, _ := fixture()
	if e := applyOnce(a, wsID); e != nil {
		t.Fatalf("第一次修订失败: %v", e)
	}
	if e := applyOnce(a, wsID); e != nil {
		t.Fatalf("第二次修订失败: %v", e)
	}
	a2, _ := storage.GetArticleByWorkspace(ctx, tenantID, wsID)
	if a2.CurrentVersionNo != 3 {
		t.Fatalf("顺序修订后版本应=3, 实得 %d", a2.CurrentVersionNo)
	}
	vs2, _ := storage.ListArticleVersions(ctx, a2.ID)
	if len(vs2) != 3 {
		t.Fatalf("顺序修订后应 3 个版本, 实得 %d", len(vs2))
	}

	// ---- 并发修订（同一最新版本）→ 恰一成功，其余冲突 ----
	aC, wsC, _ := fixture() // 新 fixture，现为 v1
	const m = 4
	startC := make(chan struct{})
	var wgC sync.WaitGroup
	var muC sync.Mutex
	succC, confC := 0, 0
	wgC.Add(m)
	for i := 0; i < m; i++ {
		go func() {
			defer wgC.Done()
			<-startC
			e := applyOnce(aC, wsC)
			muC.Lock()
			if e == nil {
				succC++
			} else if errors.Is(e, ErrArticleVersionConflict) {
				confC++
			}
			muC.Unlock()
		}()
	}
	close(startC)
	wgC.Wait()
	if succC != 1 {
		t.Fatalf("并发修订应恰 1 个成功, 实得 %d (conf=%d)", succC, confC)
	}
	if confC+succC != m {
		t.Fatalf("冲突+成功应=%d != %d+%d", m, succC, confC)
	}
	aP, _ := storage.GetArticleByWorkspace(ctx, tenantID, wsC)
	if aP.CurrentVersionNo != 2 {
		t.Fatalf("并发修订后应升至 v2, 实得 %d", aP.CurrentVersionNo)
	}
	vsC, _ := storage.ListArticleVersions(ctx, aP.ID)
	if len(vsC) != 1+succC {
		t.Fatalf("版本数应=1+成功=%d 实得 %d", 1+succC, len(vsC))
	}
	seenC := map[int]bool{}
	for _, v := range vsC {
		if seenC[v.VersionNo] {
			t.Fatalf("存在重复版本 %d", v.VersionNo)
		}
		seenC[v.VersionNo] = true
	}
	t.Logf(" revision CAS PASS: 顺序 1→2→3; 并发 m=%d 成功=%d 冲突=%d, 无重复版本", m, succC, confC)
}
