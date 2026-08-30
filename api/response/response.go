package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ============ 错误码常量 ============

const (
	CodeSuccess      = 0
	CodeBadRequest   = 400
	CodeUnauthorized = 401
	CodeForbidden    = 403
	CodeServerError  = 500
	CodeRateLimited  = 429
)

// Body 统一响应结构体：code=0 成功，非 0 失败。
type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// Success 成功返回（HTTP 200，code=0）。
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: CodeSuccess, Message: "ok", Data: data})
}

// SuccessMessage 成功返回，自定义成功提示。
func SuccessMessage(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: CodeSuccess, Message: message, Data: data})
}

// Fail 业务失败返回（HTTP 200，code 区分）。
func Fail(c *gin.Context, code int, message string) {
	c.JSON(http.StatusOK, Body{Code: code, Message: message, Data: nil})
}

// FailStatus 失败返回，自定义 HTTP 状态码。
func FailStatus(c *gin.Context, httpStatus, code int, message string) {
	c.JSON(httpStatus, Body{Code: code, Message: message, Data: nil})
}

// BadRequest 400 参数错误。
func BadRequest(c *gin.Context, message string) { Fail(c, CodeBadRequest, message) }

// BadRequestValidation 400 参数校验失败（结构化字段错误）。
func BadRequestValidation(c *gin.Context, fieldErrors map[string]string) {
	c.JSON(http.StatusOK, Body{Code: CodeBadRequest, Message: "参数校验失败", Data: fieldErrors})
}

// Unauthorized 401 未登录（返回真正的 HTTP 401）。
func Unauthorized(c *gin.Context, message string) {
	FailStatus(c, http.StatusUnauthorized, CodeUnauthorized, message)
}

// Forbidden 403 无权限。
func Forbidden(c *gin.Context, message string) { Fail(c, CodeForbidden, message) }

// ServerError 500 服务器内部错误。
func ServerError(c *gin.Context, message string) { Fail(c, CodeServerError, message) }

// TooManyRequests 429 触发限流。
func TooManyRequests(c *gin.Context, message string) {
	FailStatus(c, http.StatusTooManyRequests, CodeRateLimited, message)
}
