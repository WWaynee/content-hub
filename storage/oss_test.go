//go:build integration

package storage

import (
	"bytes"
	"testing"

	"github.com/WWaynee/content-hub/config"
)

// TestOSSUploadDownload 验证 OSS bucket my-content-hub 可连通（自动创建 + 上传下载）。
func TestOSSUploadDownload(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("加载配置失败，跳过: %v", err)
	}
	if cfg.OSS.AccessKeyID == "" || cfg.OSS.AccessKeySecret == "" {
		t.Skip("OSS 未配置真实 key，跳过")
	}
	if cfg.OSS.Bucket != "my-content-hub" {
		t.Fatalf("应使用 my-content-hub bucket，实际=%s", cfg.OSS.Bucket)
	}
	if OSSClient == nil {
		if err := InitOSS(); err != nil {
			t.Fatalf("InitOSS 失败: %v", err)
		}
	}

	key := "kbase/conn-test/hello.txt"
	content := []byte("content-hub oss connectivity test")
	if err := UploadFile(key, bytes.NewReader(content)); err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	got, err := DownloadFile(key)
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("下载内容不符: got=%q want=%q", got, content)
	}
	// 清理
	_ = DeleteFile(key)
}
