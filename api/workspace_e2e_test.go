//go:build integration

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/storage"
)

// TestWorkspaceEndpointsE2E 验证工作区列表/新建镜像端到端。
func TestWorkspaceEndpointsE2E(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("配置加载失败: %v", err)
	}
	if _, err := storage.InitMySQL(&cfg.MySQL); err != nil {
		t.Skipf("MySQL 不可用: %v", err)
	}
	if _, err := storage.InitRedis(cfg.Redis.Host, cfg.Redis.Port, cfg.Redis.Password, 0); err != nil {
		t.Skipf("Redis 不可用: %v", err)
	}

	r := NewRouter()

	// 注册租户拿 token
	regBody := []byte(`{"name":"ws测试租户","admin_name":"wsadmin","admin_passwd":"pass123456"}`)
	token := registerAndGetToken(t, r, regBody)

	// 新建工作区
	doTrack(t, r, "POST", "/api/workspaces", token, `{"title":"测试工作区"}`)

	// 列表
	listRes := doTrack(t, r, "GET", "/api/workspaces", token, "")
	var listResp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(listRes, &listResp); err != nil {
		t.Fatalf("列表解析失败: %v", err)
	}
	if len(listResp.Data) == 0 {
		t.Fatal("列表应至少 1 个工作区")
	}

	// 清理
	cleanupWS(t)
}

func registerAndGetToken(t *testing.T, r http.Handler, body []byte) string {
	req := httptest.NewRequest("POST", "/api/tenant/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Data.Token == "" {
		t.Fatalf("注册失败: %v body=%s", err, w.Body.String())
	}
	return resp.Data.Token
}

func doTrack(t *testing.T, r http.Handler, method, path, token, body string) []byte {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("%s %s 返回 %d: %s", method, path, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func cleanupWS(t *testing.T) {
	db := storage.GetDB()
	db.Exec("DELETE FROM requirements WHERE tenant_id IN (SELECT id FROM tenants WHERE name='ws测试租户')")
	db.Exec("DELETE FROM workspaces WHERE tenant_id IN (SELECT id FROM tenants WHERE name='ws测试租户')")
	db.Exec("DELETE FROM users WHERE tenant_id IN (SELECT id FROM tenants WHERE name='ws测试租户')")
	db.Exec("DELETE FROM tenants WHERE name='ws测试租户'")
}
