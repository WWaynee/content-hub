package handler

// run_gen.go -- P13：把"稿件生成主链"改为 API 进程内后台 goroutine 逐步执行，
// 并把"进展到第几步 / 这步在干嘛 / 发出了什么、收回了什么 / 完成还是卡住"落地 agent_steps
// 同时经进程内 Broker 广播，供 Steps(轮询) 与 /generate/stream(SSE) 显示给前端。
//
// 设计要点：
//   - 不用 HTTP request ctx 驱动主链（请求返回即 cancel）；主链用自带 20 分钟超时的后台 ctx。
//   - DB(agent_runs/agent_steps) 是进度的事实源；SSE 只做低延迟推送（断线仍可由 DB 回放）。
//   - launchGenerationBackground 在进程内起 goroutine，不 MQ 化（真异步队列留 P13b）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/agent"
	agentcensor "github.com/WWaynee/content-hub/agent/censor"
	"github.com/WWaynee/content-hub/agent/evidence"
	"github.com/WWaynee/content-hub/agent/orchestrator"
	"github.com/WWaynee/content-hub/agent/progress"
	"github.com/WWaynee/content-hub/agent/retrieve"
	"github.com/WWaynee/content-hub/agent/writing"
	"github.com/WWaynee/content-hub/api/middleware"
	"github.com/WWaynee/content-hub/api/response"
	"github.com/WWaynee/content-hub/api/service"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/observability"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// generationTask 一次后台生成所需上下文（前置校验/范围展开在 handler 前置同步完成）。
type generationTask struct {
	RunID            uint64
	TenantID         uint64
	UserID           uint64
	WorkspaceID      uint64
	Requirement      agent.Requirement
	RequirementID    uint64
	RequirementVersion int
	FileIDs          []uint64
	PrevStatus       string
}

// runBroker 进程内单例 Broker（SSE 读取方与生成写入方共用；同进程即可跨 handler）。
var runBrokerOnce struct {
	b  *progress.Broker
	ok bool
}

func runBroker() *progress.Broker {
	if !runBrokerOnce.ok {
		runBrokerOnce.b = progress.NewBroker()
		runBrokerOnce.ok = true
	}
	return runBrokerOnce.b
}

// launchGenerationBackground 立即在独立 goroutine 跑生成；POST 快速返回 run_id 给前端。
func launchGenerationBackground(args generationTask) {
	go runGenerationGoroutine(args)
}

// 用户语义步序号 → 人话标题/执行者（1..4，与 orchestrator.StepXxx 对齐）。
var stepMeta = []struct {
	Title string
	Role  string
}{
	{Title: "解析需求并在知识库中检索证据", Role: model.RolePlanner},
	{Title: "依据检索到的证据撰写全文", Role: model.RoleWriter},
	{Title: "逐句核实数据/事实断言有据可查", Role: model.RoleVerifier},
	{Title: "整理证据清单并成稿", Role: model.RoleEvidence},
}

func stepTitle(no int) string {
	if no >= 1 && no <= len(stepMeta) {
		return stepMeta[no-1].Title
	}
	return "生成"
}

func stepRole(no int) string {
	if no >= 1 && no <= len(stepMeta) {
		return stepMeta[no-1].Role
	}
	return model.RolePlanner
}

// runGenerationGoroutine 执行整条主链；每一步(1..4)的变化都会落 agent_steps 并广播。
func runGenerationGoroutine(args generationTask) {
	// 用不依赖 HTTP request ctx 的后台 context，并注入 tenant/user，使“私有库可见性”成立：
	// 检索依赖 ctx 中的 user_id（observability.UserIDFromCtx）限定可见范围——若不起注入，
	// 后台 ctx 的 user=0 只能看到公库，zhumi 的私有资料（含其勾选范围）将全部查不到而误报缺证。
	base := context.Background()
	ctx, cancel := context.WithTimeout(observability.WithTenantUser(base, args.TenantID, args.UserID), 20*time.Minute)
	defer cancel()
	broker := runBroker()

	defer func() {
		if r := recover(); r != nil {
			_ = storage.FailRun(context.Background(), args.RunID, fmt.Sprint("生成进程异常：", r))
			_ = storage.MarkAllStepsFinal(context.Background(), args.RunID)
			storage.UpdateWorkspaceStatus(context.Background(), args.WorkspaceID, args.PrevStatus)
			broker.Emit(progress.Event{RunID: args.RunID, Type: progress.EvRunFailed, Payload: "后台生成进程异常"})
		}
	}()
	// 终局兜底：无论成功/失败/中断，都把「进行中」的进度步收口为 done，保证 DB 回放一致。
	defer func() {
		_ = storage.MarkAllStepsFinal(ctx, args.RunID)
	}()

	llm := llmclient.NewClient()
	checker := agentcensor.NewClaimPlanner(llm, service.NewKbaseSearcher())
	orch := orchestrator.New(retrieve.New(llm), writing.New(llm), evidence.New(), checker).
		SetFactVerifier(agentcensor.NewFactVerifier(llm))

	// 进度桥：orchestrator 内在每阶段发起的 begin/detail/done/fail → 落库 + 广播。
	stepRow := map[int]uint64{} // 语义步序号 → agent_steps.id
	orch.SetOnStep(func(ev progress.Event) {
		if ev.StepNo < 1 || ev.StepNo > orchestrator.TotalSteps {
			return
		}
		ev.RunID = args.RunID
		var rowID uint64
		if id, ok := stepRow[ev.StepNo]; ok {
			rowID = id
		}
		p, _ := ev.Payload.(progress.Step)
		switch ev.Type {
		case progress.EvStepBegin:
			id, e := storage.BeginProgressStep(ctx, args.RunID, stepRole(ev.StepNo), "stage", stepTitle(ev.StepNo), orchestrator.TotalSteps)
			if e != nil {
				return
			}
			stepRow[ev.StepNo] = id
			broker.Emit(ev)
		case progress.EvDetail:
			if rowID == 0 {
				return
			}
			_ = storage.EndProgressStep(ctx, rowID, false, false, "", safeDetail(p.Detail, 1200), 0)
			broker.Emit(ev)
		case progress.EvStepFail:
			if rowID != 0 {
				_ = storage.EndProgressStep(ctx, rowID, true, true, p.Failure, safeDetail(p.Detail, 1200), p.DurationMs)
			}
			broker.Emit(ev)
		case progress.EvStepDone:
			if rowID != 0 {
				_ = storage.EndProgressStep(ctx, rowID, true, false, "", safeDetail(p.Detail, 1200), p.DurationMs)
			}
			broker.Emit(ev)
		}
	})

	res, err := orch.Generate(ctx, args.TenantID, args.Requirement, args.FileIDs)
	if err != nil {
		storage.FailRun(ctx, args.RunID, generationFailureText(err))
		storage.UpdateWorkspaceStatus(ctx, args.WorkspaceID, args.PrevStatus)
		broker.Emit(progress.Event{RunID: args.RunID, Type: progress.EvRunFailed, Payload: generationFailureText(err)})
		return
	}

	// 检索快照落库（惰性失效判定 + 证据追溯；失败不阻断）。
	_, rerr := service.PersistRetrievalBatch(ctx, args.TenantID, args.WorkspaceID, args.RequirementID, args.RequirementVersion,
		res.Queries, service.EvidenceToKbaseHits(res.Evidence))
	if rerr != nil {
		_ = rerr
	}

	verID, perr := service.PersistArticleSnapshot(ctx, args.TenantID, args.WorkspaceID, res.Article, res.Evidence)
	if perr != nil {
		storage.FailRun(ctx, args.RunID, "稿件落库失败: "+perr.Error())
		storage.UpdateWorkspaceStatus(ctx, args.WorkspaceID, args.PrevStatus)
		broker.Emit(progress.Event{RunID: args.RunID, Type: progress.EvRunFailed, Payload: "稿件落库失败: " + perr.Error()})
		return
	}
	_, _ = storage.AppendStep(ctx, args.RunID, model.AgentStep{
		Role: model.RoleEvidence, Action: "persist_generation_snapshot",
		Outcome: model.OutcomeAccepted, Decision: "稿件生成完成并落为新版本", RefID: verID, Done: true})
	_ = storage.FinishRunOk(ctx, args.RunID, verID)
	_ = storage.UpdateWorkspaceStatus(ctx, args.WorkspaceID, "generated")
	broker.Emit(progress.Event{RunID: args.RunID, Type: progress.EvRunDone, Payload: gin.H{"article_version_id": verID, "run_id": args.RunID}})
}

// generationFailureText 把主链错误翻译成可在进度面板展示的人话文本（复用既有健康文案）。
func generationFailureText(err error) string {
	var insuff *orchestrator.ErrInsufficientEvidence
	var factUnsup *orchestrator.ErrFactUnsupported
	switch {
	case errors.As(err, &insuff):
		return buildNoEvidenceMessage(insuff)
	case errors.As(err, &factUnsup):
		return "稿件未通过事实校验，部分数据在知识库中无证据支撑，已禁止生成：\n" + buildUnsupportedMessage(factUnsup)
	case errors.Is(err, orchestrator.ErrNoEvidence):
		return "知识库中未检索到与该需求主题相关的资料，无法生成含具体数据的稿件。请先在知识库补充相关文档资料，或调整需求单内容后重试。"
	default:
		return "生成失败：" + err.Error()
	}
}

// safeDetail 限制 detail 长度（脱敏/截断，P13 不存超长原文）。
func safeDetail(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// GetGenerationRun 返回某工作区最近一次 visible run + steps（供前端轮询/回放断线重建）。
func GetGenerationRun(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		response.BadRequest(c, "无效工作区 ID")
		return
	}
	runIDQ := strings.TrimSpace(c.Query("run_id"))
	var run *model.AgentRun
	if runIDQ != "" {
		if id, e := strconv.ParseUint(runIDQ, 10, 64); e == nil {
			if r, ge := storage.GetRun(c.Request.Context(), tenantID, id); ge == nil {
				run = r
			}
		}
	}
	if run == nil {
		// 未指定或无效 run_id：返回该 ws 当前 active（进行中 / 等人工）run；无则最近 success/failed
		if a, ae := storage.ListActiveRun(c.Request.Context(), tenantID, wid); ae == nil {
			run = a
		} else if list, le := storage.ListRunsByWorkspace(c.Request.Context(), tenantID, wid); le == nil && len(list) > 0 {
			run = &list[0]
		}
	}
	if run == nil {
		response.Success(c, gin.H{"run": nil})
		return
	}
	steps, _ := storage.ListSteps(c.Request.Context(), run.ID)
	response.Success(c, gin.H{
		"run":   run,
		"steps": steps,
	})
}

// StreamGeneration SSE：连接后先回放该 run 已落 steps（断线重建），再订阅 Broker 增量直到终态。
// 仅支持同进程（P13 范围内 worker 非 MQ；跨进程流式分发留给 P13b）。
func StreamGeneration(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	wid, err := parseID(c.Param("workspace_id"))
	if err != nil {
		c.String(http.StatusBadRequest, "invalid workspace")
		return
	}
	_ = wid
	runIDQ := strings.TrimSpace(c.Query("run_id"))
	if runIDQ == "" {
		c.String(http.StatusBadRequest, "run_id required")
		return
	}
	rid, rerr := strconv.ParseUint(runIDQ, 10, 64)
	if rerr != nil {
		c.String(http.StatusBadRequest, "invalid run_id")
		return
	}
	run, ge := storage.GetRun(c.Request.Context(), tenantID, rid)
	if ge != nil {
		c.String(http.StatusNotFound, "run not found")
		return
	}

	fl, ok := c.Writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming unsupported")
		return
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	writeSSE := func(e progress.Event) {
		b, _ := json.Marshal(e)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", e.Type, b)
		fl.Flush()
	}

	// 若 run 尚未终态：订阅 Broker 增量（Broker 自带最近 Recent 条的回放，能追平到实时；
	// 语义步用 step 1..4，不再把 DB 里 P05 的 start/persist 行当作卡回放，避免与实时步序错位）。
	if run.Status == string(model.RunRunning) || run.Status == string(model.RunAwaitingHuman) {
		ch := runBroker().Subscribe(rid)
		defer runBroker().Unsubscribe(rid, ch)
		keepGoing := true
		for keepGoing {
			select {
			case <-c.Request.Context().Done():
				keepGoing = false
			case ev, okCh := <-ch:
				if !okCh {
					keepGoing = false
					break
				}
				writeSSE(ev)
				if ev.Type == progress.EvRunDone || ev.Type == progress.EvRunFailed {
					keepGoing = false
				}
			}
		}
		return
	}
	// 已终态：直接给一个结束帧（前端用 fetch 流解析；无增量可推）。
	if run.Status == string(model.RunSuccess) {
		writeSSE(progress.Event{RunID: rid, Type: progress.EvRunDone, Payload: gin.H{"article": true, "run_id": rid}})
	} else {
		writeSSE(progress.Event{RunID: rid, Type: progress.EvRunFailed, Payload: run.ErrorMsg})
	}
}

// stAsStep 把一个 agent_step 归档转成前端进度 Step 视图（无 P13 新列的旧行优雅降级）。
func stAsStep(s model.AgentStep) progress.Step {
	title := s.StepTitle
	if title == "" {
		title = s.Action
		if title == "" {
			title = s.Role
		}
	}
	out := progress.Step{
		No:         s.StepNo,
		Total:      s.TotalSteps,
		Role:       s.Role,
		Title:      title,
		Done:       s.Done,
		DurationMs: s.DurationMs,
		Detail:     s.Detail,
	}
	if s.Failure != "" {
		out.Failed, out.Failure = true, s.Failure
	}
	return out
}

// 结束 run 的 SSE 通过 run_done/failed 事件驱动前端 performFinal（见 web）。
