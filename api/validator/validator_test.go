package validator

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBindJSONValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type req struct {
		Username string `json:"username" binding:"required,min=3"`
		Age      int    `json:"age" binding:"min=1"`
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"username":"ab","age":0}`))
	c.Request.Header.Set("Content-Type", "application/json")

	var r req
	err := BindJSON(c, &r)
	if err == nil {
		t.Fatal("应返回校验错误")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("应为 ValidationError，实际 %T", err)
	}
	if ve.Fields["username"] == "" {
		t.Errorf("应有 username 字段错误，实际 %+v", ve.Fields)
	}
	if ve.Fields["age"] == "" {
		t.Errorf("应有 age 字段错误，实际 %+v", ve.Fields)
	}
}

func TestBindJSONValid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type req struct {
		Username string `json:"username" binding:"required,min=3"`
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"username":"abc"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	var r req
	if err := BindJSON(c, &r); err != nil {
		t.Fatalf("合法请求应通过，实际 %v", err)
	}
	if r.Username != "abc" {
		t.Errorf("应绑定 username=abc，实际 %q", r.Username)
	}
}

func TestHandleBindError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	HandleBindError(c, &ValidationError{Fields: map[string]string{"username": "长度不能少于 3 个字符"}})
	if w.Code != http.StatusOK {
		t.Errorf("应返回 200(业务错误)，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "参数校验失败") {
		t.Errorf("响应应含校验失败信息，实际 %s", w.Body.String())
	}
}
