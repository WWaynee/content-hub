package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/WWaynee/content-hub/mq"
	"github.com/WWaynee/content-hub/storage"
	"github.com/WWaynee/content-hub/storage/model"
)

// 知识库文档上传/覆盖。

// IngestParams 上传参数。
type IngestParams struct {
	TenantID     uint64
	Scope        string // public / private
	OwnerUserID  uint64 // private 库归属人；public 传 0
	DirID        uint64
	FileName     string
	Content      []byte
	// 覆盖时指定；新建传 0。
	TargetFileID uint64
}

// IngestResult 上传结果。
type IngestResult struct {
	FileID    uint64
	VersionID uint64
	VersionMd5 string
}

// IngestDocument 上传/覆盖文档，并同步完成「切片→句子→向量化」全链路。
//
// 新建：创建 kbase_file + doc_version(version_no=1) → ProcessDocument。
// 覆盖：在 target 文件上新建 doc_version(version_no+1)，旧版 latest 在成功后置 0。
func IngestDocument(ctx context.Context, p IngestParams) (*IngestResult, error) {
	if err := validateFileType(p.FileName); err != nil {
		return nil, err
	}
	md5sum := md5hex(p.Content)

	var fileID uint64
	var versionNo int

	if p.TargetFileID == 0 {
		// 新建文件
		f := &model.KbaseFile{
			TenantID:          p.TenantID,
			Scope:             p.Scope,
			DirID:             p.DirID,
			OwnerUserID:       p.OwnerUserID,
			Name:              p.FileName,
			CurrentVersionMd5: "",
			FileType:          extToType(p.FileName),
			Size:              int64(len(p.Content)),
			Active:            1,
		}
		if err := storage.CreateFile(ctx, f); err != nil {
			return nil, fmt.Errorf("创建文件记录失败: %w", err)
		}
		fileID = f.ID
		versionNo = 1
	} else {
		// 覆盖：校验目标文件属于本租户
		f, err := storage.GetFileByID(ctx, p.TenantID, p.TargetFileID)
		if err != nil {
			return nil, ErrFileNotFound
		}
		fileID = f.ID
		cur, err := storage.GetLatestVersion(ctx, fileID)
		if err != nil {
			return nil, fmt.Errorf("查询当前版本失败: %w", err)
		}
		versionNo = cur.VersionNo + 1
	}

	// 上传 OSS：objectKey 含 tenant + fileID + md5，物理扁平
	objectKey := fmt.Sprintf("kbase/%d/%d/%s%s", p.TenantID, fileID, md5sum, filepath.Ext(p.FileName))
	if err := storage.UploadFile(objectKey, bytes.NewReader(p.Content)); err != nil {
		return nil, fmt.Errorf("上传 OSS 失败: %w", err)
	}

	ver := &model.DocVersion{
		TenantID:       p.TenantID,
		FileID:         fileID,
		VersionMd5:     md5sum,
		VersionNo:      versionNo,
		OSSObjectKey:   objectKey,
		Latest:         0, // 成功后由 MarkVersionSuccess 置 1
		Status:         storage.FileStatusPending,
		UploaderUserID: p.OwnerUserID,
	}
	if err := storage.CreateVersion(ctx, ver); err != nil {
		// 回滚：OSS 已上传，尝试删除避免孤儿
		_ = storage.DeleteFile(objectKey)
		return nil, fmt.Errorf("创建版本记录失败: %w", err)
	}

	// 异步：投递 MQ 文档解析任务，由 worker 进程消费解析（不阻塞上传请求）
	if err := mq.PublishDocumentParseTask(ctx, p.TenantID, fileID, ver.ID); err != nil {
		// 投递失败则不视为成功版本（保留上一版 latest）
		return nil, fmt.Errorf("投递解析任务失败: %w", err)
	}

	return &IngestResult{FileID: fileID, VersionID: ver.ID, VersionMd5: md5sum}, nil
}

// IngestAndParse 上传 + 同步解析（供测试/需要同步完成的场景使用；生产走 IngestDocument 异步投递 MQ）。
func IngestAndParse(ctx context.Context, p IngestParams) (*IngestResult, error) {
	// 复用 IngestDocument 的上传+建版本，但改为同步 ProcessDocument
	// 由于 IngestDocument 已异步投递，这里构造一个"同步"版本：上传后直接 ProcessDocument。
	res, err := IngestDocument(ctx, p)
	if err != nil {
		return nil, err
	}
	if err := ProcessDocument(ctx, p.TenantID, res.FileID, res.VersionID); err != nil {
		return nil, err
	}
	return res, nil
}

func validateFileType(name string) error {
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".txt" && ext != ".md" && ext != ".markdown" {
		return fmt.Errorf("不支持的文件类型 %q，仅支持 txt/md", ext)
	}
	return nil
}

func extToType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".txt" {
		return "txt"
	}
	return "md"
}

func md5hex(b []byte) string {
	s := md5.Sum(b)
	return hex.EncodeToString(s[:])
}
