package service

import (
	"context"
	"fmt"

	"github.com/WWaynee/content-hub/agent/qabot"
	"github.com/WWaynee/content-hub/llmclient"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 知识库问答业务层（独立会话）。

// CreateQASession 新建问答会话（标题默认取 "新会话"，后续首问后可更新）。
func CreateQASession(ctx context.Context, tenantID, userID uint64) (*model.QASession, error) {
	s := &model.QASession{TenantID: tenantID, UserID: userID, Title: "新会话"}
	if err := storage.CreateQASession(ctx, s); err != nil {
		return nil, fmt.Errorf("创建问答会话失败: %w", err)
	}
	return s, nil
}

// AskQABot 在会话内提问：检索 + 回答 + 落消息。
func AskQABot(ctx context.Context, tenantID, userID, sessionID uint64, question string) (*model.QAMessage, error) {
	// 校验会话归属
	if _, err := storage.GetQASession(ctx, tenantID, userID, sessionID); err != nil {
		return nil, fmt.Errorf("会话不存在")
	}
	// 落用户消息
	um := &model.QAMessage{SessionID: sessionID, TenantID: tenantID, UserID: userID, Role: "user", Content: question}
	if err := storage.AppendQAMessage(ctx, um); err != nil {
		return nil, err
	}

	// 问答 agent（注入检索实现）
	bot := qabot.New(llmclient.NewClient(), &kbaseRetriever{})
	res, err := bot.Answer(ctx, tenantID, question)
	if err != nil {
		return nil, err
	}

	// 落助手消息
	am := &model.QAMessage{SessionID: sessionID, TenantID: tenantID, UserID: userID, Role: "assistant", Content: res.Answer}
	if err := storage.AppendQAMessage(ctx, am); err != nil {
		return nil, err
	}

	// 首次提问则用问题前若干字更新标题
	title := question
	if len([]rune(title)) > 20 {
		title = string([]rune(title)[:20])
	}
	storage.UpdateQASessionTitle(ctx, sessionID, title)

	return am, nil
}

// kbaseRetriever 实现 qabot.Retriever，复用 SearchKbase（切片级检索）。
type kbaseRetriever struct{}

func (k *kbaseRetriever) Retrieve(ctx context.Context, tenantID uint64, query string) ([]string, error) {
	evs, err := SearchKbase(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}
	texts := make([]string, 0, len(evs))
	for _, e := range evs {
		texts = append(texts, e.SourceText)
	}
	return texts, nil
}
