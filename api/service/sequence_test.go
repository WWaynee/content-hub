package service

import (
	"strings"
	"testing"

	"github.com/WWaynee/content-hub/storage/model"
)

// sequence_test.go — P08 纯序列执行器单测（无 DB / 无 LLM）。
// 覆盖 applyChangePlan + reflowSents 的确定性部分：edit/insert/delete/move 的顺序、证据继承、
// no_source 提醒、closed-set 不变式。真实 CAS + DB 落库另由 *_integration_test.go（真 MySQL）承担。

func mkSrcSet(bindWith ...uint64) ([]model.ArticleSentence, map[uint64][]model.EvidenceBinding) {
	lbl := []string{"A", "B", "C", "D", "E"}
	var sents []model.ArticleSentence
	for id := uint64(1); id <= 5; id++ {
		sents = append(sents, model.ArticleSentence{
			ID: id, SectionIndex: 0, ParagraphIndex: 0, SentenceIndex: int(id - 1),
			Content: "句" + lbl[id-1],
		})
	}
	mp := map[uint64][]model.EvidenceBinding{}
	for _, id := range bindWith {
		mp[id] = append(mp[id], model.EvidenceBinding{
			ArticleSentenceID: id, SourceType: "knowledge",
			DocFileID: 100, DocSentenceID: 1000 + id, EvidenceStatus: "bound",
		})
	}
	return sents, mp
}

// planContents / planSrcOrder 断言辅助
func planContents(p *seqPlan) []string {
	out := make([]string, 0, len(p.sents))
	for _, s := range p.sents {
		out = append(out, s.content)
	}
	return out
}
func planSrcOrder(p *seqPlan) []uint64 {
	var out []uint64
	for _, s := range p.sents {
		if s.srcID != 0 {
			out = append(out, s.srcID)
		}
	}
	return out
}

func TestChangePlan_Edit_KeepsParaAndPreferBindingByDefault(t *testing.T) {
	src, bind := mkSrcSet(2)
	p, err := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "edit", TargetID: 2, NewText: "句B改过"},
	}}, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.sents) != 5 {
		t.Fatalf("edit 不改变句子数,got %d", len(p.sents))
	}
	if p.sents[1].content != "句B改过" || p.sents[1].srcID != 2 {
		t.Fatalf("target 句未被正确更新:%+v", p.sents[1])
	}
	if p.sents[0].content != "句A" || p.sents[4].content != "句E" {
		t.Errorf("未动句被改变:%v", planContents(p))
	}
	if p.sents[1].sec != 0 || p.sents[1].para != 0 {
		t.Errorf("edit 不应动段落归属")
	}
	if len(p.sents[1].binds) != 1 || p.sents[1].binds[0].DocSentenceID != 1002 {
		t.Fatalf("默认应保留原绑定:%+v", p.sents[1].binds)
	}
	// closed-set & 有序
	if got := planSrcOrder(p); !eqUint(got, []uint64{1, 2, 3, 4, 5}) {
		t.Errorf("closed-set broke:%v", got)
	}
}

func TestChangePlan_Edit_BindingPolicy(t *testing.T) {
	src, bind := mkSrcSet(2)
	// clear
	p1, _ := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "edit", TargetID: 2, NewText: "句Bx", ClearEvidence: true},
	}}, 9)
	if len(p1.sents[1].binds) != 0 {
		t.Errorf("clear 后应有 0 条,got %d", len(p1.sents[1].binds))
	}
	// override with new evidence
	p2, _ := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "edit", TargetID: 2, NewText: "句By", Evidence: []ChangeEvidence{{FileID: 9, DocSentenceID: 777}}},
	}}, 9)
	if len(p2.sents[1].binds) != 1 || p2.sents[1].binds[0].DocSentenceID != 777 {
		t.Errorf("override 绑定失败:%+v", p2.sents[1].binds)
	}
}

func TestChangePlan_InsertNoSource_OrderAndMark(t *testing.T) {
	src, bind := mkSrcSet()
	p, err := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "insert", AnchorID: 2, NewText: "新句"},
	}}, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.sents) != 6 {
		t.Fatalf("insert 应 +1 -> 6")
	}
	// anchor(句B, idx1) 之后
	if p.sents[1].content != "句B" || p.sents[2].content != "新句" || p.sents[3].content != "句C" {
		t.Fatalf("insert 顺序错:%v", planContents(p))
	}
	if p.sents[2].srcID != 0 {
		t.Errorf("新增句 srcID 应为 0(无源前缀),got %d", p.sents[2].srcID)
	}
	if p.sents[2].sec != 0 || p.sents[2].para != 0 {
		t.Errorf("insert 应进 anchor 段")
	}
	if len(p.sents[2].binds) != 0 {
		t.Errorf("无证 insert 不应误带绑定")
	}
	if len(p.reviews) == 0 || !strings.Contains(strings.Join(p.reviews, " "), "no_source") {
		t.Errorf("insert 无证应产生 no_source review:%v", p.reviews)
	}
	// closed-set src: [1,2,3,4,5] 仍全在
	if !eqUint(planSrcOrder(p), []uint64{1, 2, 3, 4, 5}) {
		t.Errorf("insert 破坏 closed-set src:%v", planSrcOrder(p))
	}
}

func TestChangePlan_InsertWithEvidence_BindsToNewSentence(t *testing.T) {
	src, bind := mkSrcSet()
	p, _ := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "insert", AnchorID: 4, NewText: "新句有据", Evidence: []ChangeEvidence{{FileID: 7, DocSentenceID: 900}}},
	}}, 9)
	if len(p.sents) != 6 {
		t.Fatalf("insert with evidence")
	}
	// 找插入句(内容"新句有据")
	for _, s := range p.sents {
		if s.content == "新句有据" {
			if len(s.binds) != 1 || s.binds[0].DocSentenceID != 900 {
				t.Fatalf("新句证据未带上:%+v", s.binds)
			}
			return
		}
	}
	t.Fatal("未找到插入句")
}

func TestChangePlan_Delete_ClosesSetAndNoDangleBind(t *testing.T) {
	src, bind := mkSrcSet(2, 4)
	p, err := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "delete", TargetID: 3},
	}}, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.sents) != 4 {
		t.Fatalf("delete 应剩 4,got %d", len(p.sents))
	}
	g := planContents(p)
	if strings.Join(g, ",") != "句A,句B,句D,句E" {
		t.Fatalf("delete 后顺序错:%v", g)
	}
	if !eqUint(planSrcOrder(p), []uint64{1, 2, 4, 5}) {
		t.Errorf("closed-set 后 src 错:%v", planSrcOrder(p))
	}
	// 被删句绑定不再有引用（D 现在在 idx2 仍有 1 条绑定）
	if len(p.sents[2].binds) != 1 {
		t.Errorf("段4原绑定应保留")
	}
	// 剩余句子上绑定归属正确——检查句B(保留 bound)
	if len(p.sents[1].binds) != 1 {
		t.Errorf("句B绑定掉落")
	}
}

func TestChangePlan_Move_KeepTextAndBindings_NotRewriteSrc(t *testing.T) {
	// 带跨段的场景更能测 reflow 归属：让句3 在 (0,0)，句4-5 在 (0,1)（新段）
	src := []model.ArticleSentence{
		{ID: 1, SectionIndex: 0, ParagraphIndex: 0, SentenceIndex: 0, Content: "句A"},
		{ID: 2, SectionIndex: 0, ParagraphIndex: 0, SentenceIndex: 1, Content: "句B"},
		{ID: 3, SectionIndex: 0, ParagraphIndex: 0, SentenceIndex: 2, Content: "句C"}, // 有绑定
		{ID: 4, SectionIndex: 0, ParagraphIndex: 1, SentenceIndex: 0, Content: "句D"},
		{ID: 5, SectionIndex: 0, ParagraphIndex: 1, SentenceIndex: 1, Content: "句E"},
	}
	bind := map[uint64][]model.EvidenceBinding{
		3: {{ArticleSentenceID: 3, SourceType: "knowledge", DocFileID: 100, DocSentenceID: 1003}},
	}
	// move 句5(E) 到 句2(B) 之后：(0,0) 段
	p, err := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "move", TargetID: 5, AnchorID: 2},
	}}, 9)
	if err != nil {
		t.Fatal(err)
	}
	g := planContents(p)
	if strings.Join(g, ",") != "句A,句B,句E,句C,句D" {
		t.Fatalf("move 顺序错:%v", g)
	}
	// E 被移入段(0,0)：其 sec/para 跟随 anchor
	for _, s := range p.sents {
		if s.content == "句E" {
			if s.sec != 0 || s.para != 0 {
				t.Errorf("move 跨段后目标应进 anchor 段,%d,%d", s.sec, s.para)
			}
			if len(s.binds) != 0 {
				t.Errorf("无据句 move 后不应因此有绑定")
			}
		}
		if s.content == "句C" {
			if len(s.binds) != 1 { // C 原绑定应跟到新位置
				t.Errorf("句C 的绑定在 move 中丢失:%d", len(s.binds))
			}
		}
	}
	if got := planSrcOrder(p); !eqUint(got, []uint64{1, 2, 5, 3, 4}) {
		t.Errorf("move 后 src 序错:%v", got)
	}
}

func TestChangePlan_ReflowSentsWithinPara(t *testing.T) {
	src, _ := mkSrcSet()
	// 删除中间句后同段句号应连续 0..n
	p, _ := applyChangePlan(src, map[uint64][]model.EvidenceBinding{}, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "delete", TargetID: 3},
	}}, 9)
	want := []int{0, 1, 2, 3}
	var got []int
	for _, s := range p.sents {
		got = append(got, s.sent)
	}
	if !eqInt(got, want) {
		t.Errorf("reflow 句号错:%v want %v", got, want)
	}
}

func TestChangePlan_InvalidOps(t *testing.T) {
	src, bind := mkSrcSet()
	if _, err := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{{Op: "frobnicate"}}}, 9); err == nil {
		t.Fatal("未知 op 应报错")
	}
	// move到自己
	if _, err := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "move", TargetID: 3, AnchorID: 3},
	}}, 9); err == nil {
		t.Fatal("move 到自己应报错")
	}
	// 锚不存在
	if _, err := applyChangePlan(src, bind, &ChangeListRequest{Ops: []ChangeOp{
		{Op: "insert", AnchorID: 999, NewText: "x"},
	}}, 9); err == nil {
		t.Fatal("ancho 不存在应报错")
	}
}

func eqUint(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func eqInt(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
