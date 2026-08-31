package storage

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"github.com/WWaynee/content-hub/config"
)

// OSSClient 全局阿里云 OSS 客户端，业务通过本包封装方法使用。
var OSSClient *oss.Client

// InitOSS 初始化阿里云 OSS 客户端（静态 AK + region + endpoint），并确保 bucket 存在。
func InitOSS() error {
	cfg := config.Get().OSS
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return fmt.Errorf("OSS 未配置 AccessKeyID/AccessKeySecret（检查 .env）")
	}
	if cfg.Bucket == "" {
		return fmt.Errorf("OSS 未配置 Bucket")
	}

	provider := credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret)
	clientCfg := oss.LoadDefaultConfig().
		WithCredentialsProvider(provider).
		WithRegion(cfg.Region).
		WithEndpoint(cfg.Endpoint)

	client := oss.NewClient(clientCfg)
	ctx := context.Background()

	exists, err := client.IsBucketExist(ctx, cfg.Bucket)
	if err != nil {
		return fmt.Errorf("检查 OSS bucket 是否存在失败: %w", err)
	}
	if !exists {
		if _, cerr := client.PutBucket(ctx, &oss.PutBucketRequest{Bucket: oss.Ptr(cfg.Bucket)}); cerr != nil {
			return fmt.Errorf("创建 OSS bucket 失败: %w", cerr)
		}
	}

	OSSClient = client
	return nil
}

// UploadFile 上传文件到 OSS。
func UploadFile(objectKey string, reader io.Reader) error {
	if OSSClient == nil {
		return fmt.Errorf("OSS 客户端未初始化")
	}
	_, err := OSSClient.PutObject(context.Background(), &oss.PutObjectRequest{
		Bucket:      oss.Ptr(getBucket()),
		Key:         oss.Ptr(objectKey),
		Body:        reader,
		ContentType: oss.Ptr(contentTypeFor(objectKey)),
	})
	if err != nil {
		return fmt.Errorf("上传文件到 OSS 失败: %w", err)
	}
	return nil
}

// DownloadFile 从 OSS 下载对象内容。
func DownloadFile(objectKey string) ([]byte, error) {
	if OSSClient == nil {
		return nil, fmt.Errorf("OSS 客户端未初始化")
	}
	res, err := OSSClient.GetObject(context.Background(), &oss.GetObjectRequest{
		Bucket: oss.Ptr(getBucket()),
		Key:    oss.Ptr(objectKey),
	})
	if err != nil {
		return nil, fmt.Errorf("打开 OSS 对象失败: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 OSS 对象内容失败: %w", err)
	}
	return data, nil
}

// DeleteFile 从 OSS 删除对象。
func DeleteFile(objectKey string) error {
	if OSSClient == nil {
		return fmt.Errorf("OSS 客户端未初始化")
	}
	_, err := OSSClient.DeleteObject(context.Background(), &oss.DeleteObjectRequest{
		Bucket: oss.Ptr(getBucket()),
		Key:    oss.Ptr(objectKey),
	})
	if err != nil {
		return fmt.Errorf("删除 OSS 对象失败: %w", err)
	}
	return nil
}

// PresignDownloadURL 生成预签名下载 URL（attachment）。
func PresignDownloadURL(objectKey, filename string, expiry time.Duration) (string, error) {
	if OSSClient == nil {
		return "", fmt.Errorf("OSS 客户端未初始化")
	}
	disposition := "attachment"
	if filename != "" {
		disposition += "; filename=\"" + filename + "\""
	}
	return presignObject(objectKey, expiry, disposition)
}

// PresignPreviewURL 生成预签名预览 URL（inline，浏览器直接打开）。
func PresignPreviewURL(objectKey string, expiry time.Duration) (string, error) {
	return presignObject(objectKey, expiry, "inline")
}

func presignObject(objectKey string, expiry time.Duration, disposition string) (string, error) {
	if OSSClient == nil {
		return "", fmt.Errorf("OSS 客户端未初始化")
	}
	req := &oss.GetObjectRequest{
		Bucket:                     oss.Ptr(getBucket()),
		Key:                        oss.Ptr(objectKey),
		ResponseContentDisposition: oss.Ptr(disposition),
	}
	result, err := OSSClient.Presign(context.Background(), req, oss.PresignExpiration(time.Now().Add(expiry)))
	if err != nil {
		return "", fmt.Errorf("生成 OSS 签名 URL 失败: %w", err)
	}
	return result.URL, nil
}

func contentTypeFor(objectKey string) string {
	switch strings.ToLower(path.Ext(objectKey)) {
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func getBucket() string { return config.Get().OSS.Bucket }
