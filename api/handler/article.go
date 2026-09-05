package handler

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/agent"
	"github.com/WWaynee/content-hub/agent/orchestrator"
	"github.com/WWaynee/content-hub/api/middleware"
	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 稿件 HTTP handler。

// GenerateArticle 触发稿件生成（generation：检索→撰写→证据→落快照）。
func GenerateArticle(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效工作区 ID")
		return
	}
	req, err := storage.GetRequirementByWorkspace(c.Request.Context(), tenantID, wid)
	if err != nil {
		response.BadRequest(c, "需求单不存在")
		return
	}
	// P12/W4：生成前先做“可生成”硬前置——缺需求项不真跑昂贵 LLM/检索，直接用人话列出还缺什么。
	// 注意：此时尚未把状态置 generating（前置在状态变更之前），故不做任何状态回退，直接响应。
	if miss := service.RequirementCompletenessIssues(req); len(miss) > 0 {
		response.Fail(c, response.CodeServerError,
			"还不能生成稿件：需求单还缺："+strings.Join(miss, "、")+"。请先在需求单补齐后再点击生成。")
		return
	}
	agentReq := toAgentRequirement(req)

	// 记录进入"生成中"前的工作区状态，供失败时回退（避免卡在 generating）
	var prevStatus string
	if ws, werr := storage.GetWorkspaceByID(c.Request.Context(), tenantID, userID, wid); werr == nil {
		prevStatus = ws.Status
	}

	// 状态机：进入生成中（禁导）
	_ = storage.UpdateWorkspaceStatus(c.Request.Context(), wid, "generating")

	// 勾选范围 → 递归展开为文件 ID，锁定检索范围（无勾选则 nil=全租户）
	fileIDs, err := service.RequirementFileIDScope(c.Request.Context(), tenantID, req.ID)
	if err != nil {
		restoreWorkspaceStatus(c, wid, prevStatus)
		response.ServerError(c, "展开勾选范围失败："+err.Error())
		return
	}

	// P05：把本次 production 固化为一个 agent_run(initial) —— 校验"同工作区已在进行中(或等待人工)的生产"
	run, runErr := beginInitialRun(c.Request.Context(), tenantID, userID, wid)
	if runErr != nil {
		restoreWorkspaceStatus(c, wid, prevStatus)
		response.Fail(c, response.CodeServerError,
			"创建生成任务失败（可能该工作区已有生成/修订在进行中）："+runErr.Error())
		return
	}
	runID := run.ID

	// P13：生成主链较长，若整个同步跑完，前端只能看到长时间 loading、不知道进度。
	// 改为：POST 先快速返回 run_id，由 API 进程内 goroutine 逐阶段把进展落地 agent_steps
	// + 广播；前端可经 Steps/SSE 实时看到"第几步、发了什么、卡在哪"。
	launchGenerationBackground(generationTask{
		RunID:       runID,
		TenantID:    tenantID,
		UserID:      userID,
		WorkspaceID: wid,
		Requirement: agentReq,
		FileIDs:     fileIDs,
		PrevStatus:  prevStatus,
		// 检索快照/惰性失效锚定到当前需求单版本（P13 后台化后依旧需要）
		RequirementID:      req.ID,
		RequirementVersion: req.Version,
	})

	response.Success(c, gin.H{
		"run_id":      runID,
		"status":      "generating",
		"total_steps": orchestrator.TotalSteps,
	})
}

// restoreWorkspaceStatus 生成失败时回退工作区状态（保持可操作，不卡死在 generating）。
func restoreWorkspaceStatus(c *gin.Context, wid uint64, prevStatus string) {
	if prevStatus == "" {
		prevStatus = "draft"
	}
	_ = storage.UpdateWorkspaceStatus(c.Request.Context(), wid, prevStatus)
}

// buildNoEvidenceMessage 把缺证清单组装成给用户的明确提示（P06 Q1：不冷冰冰报错,
// 收敛成一句带三条选择的人话,对应 Guardian ask_human 的心智,避免"无据=整稿红"。)
func buildNoEvidenceMessage(insuff *orchestrator.ErrInsufficientEvidence) string {
	var sb strings.Builder
	sb.WriteString("以下内容需要资料/数据支撑，但当前知识库暂时检索不到可靠来源：")
	for _, m := range insuff.Missing {
		show := m.Text
		if m.QueryHint != "" {
			show += "（建议检索：”" + m.QueryHint + "“）"
		}
		sb.WriteString("\n· " + show)
	}
	sb.WriteString("\n\n你希望怎么处理？(a) 我先去资料库补相关资料再重新生成；" +
		"(b) 把无法支撑的部分去掉、只保留有据内容生成；" +
		"(c) 放弃本次生成。")
	return sb.String()
}

// buildUnsupportedMessage 把无法获得证据支撑的数据断言清单组装成提示。
func buildUnsupportedMessage(unsup *orchestrator.ErrFactUnsupported) string {
	var sb strings.Builder
	sb.WriteString("存在无法在知识库证据原文中找到支撑的数据断言：")
	for _, u := range unsup.Unsupported {
		sb.WriteString("\n· " + u)
	}
	sb.WriteString("\n请补充相关资料，或调整需求后重试。")
	return sb.String()
}

// GetArticle 读取稿件（含句子 + 证据标注）。
func GetArticle(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效工作区 ID")
		return
	}
	a, err := storage.GetArticleByWorkspace(c.Request.Context(), tenantID, wid)
	if err != nil {
		response.BadRequest(c, "稿件不存在")
		return
	}
	ver, err := storage.GetLatestArticleVersion(c.Request.Context(), a.ID)
	if err != nil {
		response.BadRequest(c, "稿件版本不存在")
		return
	}
	sents, _ := storage.ListArticleSentences(c.Request.Context(), ver.ID)
	binds, _ := storage.ListArticleBindings(c.Request.Context(), ver.ID)

	// P04：装配"人读 source"并生成 sentence_views（RFC rev-2 §10.1 / rev-4 W6）
	// 每条证据带原句引文/文档名/章节/版本/has_newer(资料有新版)/file_deleted(文档已删)，
	// 让 tooltip 与"可溯源"不再只是裸 doc_sentence_id。旧字段(bindings/sentences)保留以兼容现前端。
	sourceBySent := service.LoadSentenceSources(c.Request.Context(), tenantID, binds)
	// P09：把某句被落成"无源待核(no_source) / 人工认可保留(human_kept)"的占位标记给装配层而不是仅按有无 sources 判定。
	statusBySent := service.ClaimStatusBySent(binds)
	sentenceViews := service.BuildSentenceViews(sents, sourceBySent, statusBySent)

	response.Success(c, gin.H{
		"article_id":         a.ID,
		"article_version_id": ver.ID,
		"title":              a.Title,
		"full_content":       ver.FullContent,
		"sentences":          sents,
		"sections":           groupSentencesByStructure(sents), // rev2-P11/rev4-W1: 结构化层级(旧版线性退化见实现)
		"sentence_views":     sentenceViews,                    // rev2-P04: 逐句人读 source(claim_type + sources)
		"bindings":           binds,
	})
}

// groupSentencesByStructure 把某版本的句子行映射回"section→paragraph→sentence"三级结构。
// 数据来自 DB 中真实写入的 section/paragraph_index(P03 生成写真实结构)。对旧版本(三 index 缺失、
// 退化为全部 0 的线性旧数据)统一当成一个线性容器：单 section、按段落 index/顺序拆分，供前端展示，
// 不做任何启发式猜标题(标题解析放 P11/后端不在此)。
func groupSentencesByStructure(sents []model.ArticleSentence) []gin.H {
	type paraView struct {
		paragraph_index int
		sentences       []gin.H
	}
	var out []gin.H
	bySec := map[int]map[int][]gin.H{} // section -> (para -> sentenceViews)
	for _, s := range sents {
		sViews := gin.H{
			"sentence_index": s.SentenceIndex,
			"id":             s.ID,
			"content":        s.Content,
		}
		if _, ok := bySec[s.SectionIndex]; !ok {
			bySec[s.SectionIndex] = map[int][]gin.H{}
		}
		bySec[s.SectionIndex][s.ParagraphIndex] = append(bySec[s.SectionIndex][s.ParagraphIndex], sViews)
	}
	// 输出有序(section asc)。paragraph 内为 append 序(已是三元排序后输入)。
	for si := 0; si <= maxKey(bySec); si++ {
		paras, ok := bySec[si]
		if !ok {
			continue
		}
		var parasView []gin.H
		for pi := 0; pi <= maxKeyParas(paras); pi++ {
			pv, ok := paras[pi]
			if !ok {
				continue
			}
			parasView = append(parasView, gin.H{
				"paragraph_index": pi,
				"sentences":       pv,
			})
		}
		out = append(out, gin.H{
			"section_index": si,
			"paragraphs":    parasView,
		})
	}
	return out
}

func maxKey(m map[int]map[int][]gin.H) int {
	max := -1
	for k := range m {
		if k > max {
			max = k
		}
	}
	return max
}

func maxKeyParas(m map[int][]gin.H) int {
	max := -1
	for k := range m {
		if k > max {
			max = k
		}
	}
	return max
}

// ExportArticle 导出稿件（合并 md）。
func ExportArticle(c *gin.Context) {
	vid, err := parseID(c.Param("article_version_id"))
	if err != nil {
		response.BadRequest(c, "无效稿件版本 ID")
		return
	}
	md, err := service.ExportArticle(c.Request.Context(), vid)
	if err != nil {
		response.ServerError(c, "导出失败："+err.Error())
		return
	}
	response.Success(c, gin.H{"markdown": md})
}

func toAgentRequirement(r *model.Requirement) agent.Requirement {
	return agent.Requirement{
		Title:              r.Title,
		Platforms:          jsonToStringSlice(r.Platforms),
		StyleTone:          r.StyleTone,
		StyleEmotion:       r.StyleEmotion,
		StyleAudience:      r.StyleAudience,
		StylePurpose:       r.StylePurpose,
		StyleTaboo:         r.StyleTaboo,
		StyleSubject:       r.StyleSubject,
		WordCount:          r.WordCount,
		ChapterRequirement: r.ChapterRequirement,
	}
}

// jsonToStringSlice 把 datatypes.JSON 解析为 []string。
func jsonToStringSlice(j interface{}) []string {
	b, err := json.Marshal(j)
	if err != nil {
		return nil
	}
	var out []string
	_ = json.Unmarshal(b, &out)
	return out
}

// applySequenceRequest 一次 change_list 请求 body。
type applySequenceBody struct {
	BaseArticleVersion int                `json:"base_article_version,omitempty"`
	Govern             bool               `json:"govern,omitempty"`
	Ops                []service.ChangeOp `json:"ops"`
}

// HandleArticleSequence PATCH /workspaces/:workspace_id/article/sequence —— 受控序列编辑(增/删/移/改一句的新句落版)
// 走「RunSequence」持久 run + change_list(CAS) 语义,产出新 article_version。
func HandleArticleSequence(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	userID := middleware.GetUserID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效工作区 ID")
		return
	}
	var body applySequenceBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "参数解析失败："+err.Error())
		return
	}
	req := &service.ChangeListRequest{BaseArticleVersion: body.BaseArticleVersion, Ops: body.Ops, Govern: body.Govern}
	verID, reviews, rerr := service.RunSequenceEdit(c.Request.Context(), tenantID, userID, wid, req)
	if rerr != nil {
		if errors.Is(rerr, service.ErrArticleVersionConflict) || errors.Is(rerr, service.ErrSequenceConflict) {
			response.Fail(c, response.CodeVersionConflict, service.ErrSequenceConflict.Error())
			return
		}
		if errors.Is(rerr, service.ErrRunActive) {
			response.Fail(c, response.CodeServerError, service.ErrRunActive.Error())
			return
		}
		response.ServerError(c, "序列编辑失败："+rerr.Error())
		return
	}
	response.Success(c, gin.H{"article_version_id": verID, "reviews": reviews, "human_text": buildSeqHuman(reviews, verID)})
}

func buildSeqHuman(reviews []string, _ uint64) string {
	if len(reviews) == 0 {
		return "已完成本次调整，并生成了新版本稿件。"
	}
	return "已完成本次调整（生成新版本）。注意：\n" + "- " + strings.Join(reviews, "\n- ")
}

// markSentenceBody PATCH /workspaces/:wid/article/mark —— 对一句"no_source 黄点"做作者人工取舍。
type markSentenceBody struct {
	SentenceID uint64 `json:"sentence_id"`
	Action     string `json:"action"` // ack_human | keep_no_source | reset_no_source
}

// HandleArticleSentenceMark 把某句在无源待核与"作者人工认可保留(无黄点)"之间做状态切换。
func HandleArticleSentenceMark(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效工作区 ID")
		return
	}
	var body markSentenceBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "参数解析失败："+err.Error())
		return
	}
	if body.SentenceID == 0 || body.Action == "" {
		response.BadRequest(c, "缺少 sentence_id / action")
		return
	}
	if err := service.MarkSentenceManual(c.Request.Context(), tenantID, wid, body.SentenceID, body.Action); err != nil {
		if errors.Is(err, service.ErrMarkHasRealSrc) {
			response.BadRequest(c, "该句已有真实引用来源，不接受直接降为无外部依据；请先编辑去掉来源再标注。")
			return
		}
		if errors.Is(err, service.ErrMarkNotExist) {
			response.BadRequest(c, "该句在当前稿件版本中不存在（可能已被删除）。")
			return
		}
		response.ServerError(c, "设置句子状态失败："+err.Error())
		return
	}
	response.SuccessMessage(c, "已更新该句的源标注", nil)
}

// governBody POST 治理一句待核的手编文本（P09 治理补足）。
type governBody struct {
	Text string `json:"text"`
}

// HandleManualGovern 对一句新插入/改写的手编文本跑治理，返回 bound/no_source/plausible 三态结论及其可引来源。
func HandleManualGovern(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效工作区 ID")
		return
	}
	var body governBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.BadRequest(c, "参数解析失败："+err.Error())
		return
	}
	res, gerr := service.GovernManualSentence(c.Request.Context(), tenantID, wid, body.Text)
	if gerr != nil {
		response.ServerError(c, "治理失败："+gerr.Error())
		return
	}
	response.Success(c, gin.H{
		"claim_type": res.ClaimType,
		"sources":    res.Sources,
		"human_text": res.HumanText,
		"fallback":   res.Fallback,
	})
}
