package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// 轻量结构化 JSON 日志出口（第一阶段基础版，后续可扩展文件/采集）。
// 字段：level/time/trace_id/tenant_id/user_id + 可选告警字段。

type key string

const (
	ctxTrace  key = "trace_id"
	ctxTenant key = "tenant_id"
	ctxUser   key = "user_id"
)

// WithTraceID 往 ctx 写入 trace_id。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, ctxTrace, traceID)
}

// TraceIDFromCtx 从 ctx 取 trace_id。
func TraceIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxTrace).(string); ok {
		return v
	}
	return ""
}

// WithTenantUser 往 ctx 写入 tenant_id / user_id。
func WithTenantUser(ctx context.Context, tenantID, userID uint64) context.Context {
	ctx = context.WithValue(ctx, ctxTenant, tenantID)
	ctx = context.WithValue(ctx, ctxUser, userID)
	return ctx
}

// TenantIDFromCtx / UserIDFromCtx 取当前操作者。
func TenantIDFromCtx(ctx context.Context) uint64 {
	if v, ok := ctx.Value(ctxTenant).(uint64); ok {
		return v
	}
	return 0
}

func UserIDFromCtx(ctx context.Context) uint64 {
	if v, ok := ctx.Value(ctxUser).(uint64); ok {
		return v
	}
	return 0
}

// logEntry 单条结构化日志。
type logEntry struct {
	Level    string `json:"level"`
	Time     string `json:"time"`
	Message  string `json:"message"`
	TraceID  string `json:"trace_id,omitempty"`
	TenantID uint64 `json:"tenant_id,omitempty"`
	UserID   uint64 `json:"user_id,omitempty"`
	// 额外字段
	Extras map[string]interface{} `json:"-"`
}

// Logger 绑定 ctx 的日志器。
type Logger struct {
	ctx    context.Context
	prefix string
}

// WithContext 返回绑定 ctx（自动注入 trace/tenant/user 字段）的 Logger。
func WithContext(ctx context.Context) *Logger { return &Logger{ctx: ctx} }

// WithAgentContext 预留：绑定 agent 链路上下文；当前同 WithContext。
func WithAgentContext(ctx context.Context) *Logger { return &Logger{ctx: ctx} }

func (l *Logger) emit(level, msg string, extra map[string]interface{}) {
	e := logEntry{
		Level:    level,
		Time:     time.Now().Format(time.RFC3339),
		Message:  msg,
		TraceID:  TraceIDFromCtx(l.ctx),
		TenantID: TenantIDFromCtx(l.ctx),
		UserID:   UserIDFromCtx(l.ctx),
		Extras:   extra,
	}
	// 合并 extras 后整体输出
	out := map[string]interface{}{
		"level": e.Level, "time": e.Time, "message": e.Message,
	}
	if e.TraceID != "" {
		out["trace_id"] = e.TraceID
	}
	if e.TenantID != 0 {
		out["tenant_id"] = e.TenantID
	}
	if e.UserID != 0 {
		out["user_id"] = e.UserID
	}
	for k, v := range extra {
		out[k] = v
	}
	b, _ := json.Marshal(out)
	loggerPrintln(string(b))
}

// Info / Warn / Error 便捷方法。
func (l *Logger) Info(msg string, extra map[string]interface{})            { l.emit("info", msg, extra) }
func (l *Logger) Warn(msg string, extra map[string]interface{})            { l.emit("warn", msg, extra) }
func (l *Logger) Error(msg string, extra map[string]interface{})           { l.emit("error", msg, extra) }
func (l *Logger) Debug(msg string, extra map[string]interface{})           { l.emit("debug", msg, extra) }

func loggerPrintln(line string) { fmt.Fprintln(os.Stdout, line) }
