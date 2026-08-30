package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/WWaynee/content-hub/config"
	"github.com/WWaynee/content-hub/util"
)

func newTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user_id":   GetUserID(c),
			"tenant_id": GetTenantID(c),
			"role":      GetRole(c),
		})
	}
	r.GET("/ping", JWTAuth(), h)
	return r
}

func doReq(t *testing.T, r *gin.Engine, token string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ping", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

func loadSecret(t *testing.T) {
	t.Helper()
	if _, err := config.Load(); err != nil {
		t.Skipf("加载配置失败，跳过: %v", err)
	}
	if len(config.Get().JWT.Secret) < 16 {
		t.Skip("JWT_SECRET 未配置足够长度，跳过")
	}
}

func TestJWTAuth_NoToken(t *testing.T) {
	loadSecret(t)
	r := newTestRouter()
	w := doReq(t, r, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401, 实际=%d", w.Code)
	}
}

func TestJWTAuth_InvalidToken(t *testing.T) {
	r := newTestRouter()
	w := doReq(t, r, "bad.token.value")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("非法 token 应 401, 实际=%d", w.Code)
	}
}

func TestJWTAuth_ValidToken(t *testing.T) {
	loadSecret(t)
	tok, err := util.GenerateToken(11, 5, "member")
	if err != nil {
		t.Fatalf("GenerateToken 失败: %v", err)
	}
	r := newTestRouter()
	w := doReq(t, r, tok)
	if w.Code != http.StatusOK {
		t.Fatalf("有效 token 应 200, 实际=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if body == "" {
		t.Fatal("应返回 identity")
	}
}
