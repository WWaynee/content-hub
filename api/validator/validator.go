package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/WWaynee/content-hub/api/response"
)

// 统一参数校验（go-playground/validator/v10）。

var Engine = validator.New()

func init() {
	Engine.SetTagName("binding")
	Engine.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" {
			name = strings.SplitN(fld.Tag.Get("form"), ",", 2)[0]
		}
		return name
	})
}

// ValidationError 结构化校验错误（字段名 → 中文提示）。
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	return fmt.Sprintf("参数校验失败，字段: %v", keys)
}

// BindJSON 绑定并校验 JSON 请求体。
func BindJSON(c *gin.Context, obj any) error {
	if c.Request == nil || c.Request.Body == nil {
		return errors.New("缺少请求体")
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return errors.New("请求体不能为空")
	}
	if err := json.Unmarshal(body, obj); err != nil {
		return err
	}
	if err := Engine.Struct(obj); err != nil {
		var verrs validator.ValidationErrors
		if errors.As(err, &verrs) {
			return &ValidationError{Fields: translate(verrs)}
		}
		return err
	}
	return nil
}

func translate(verrs validator.ValidationErrors) map[string]string {
	fields := make(map[string]string, len(verrs))
	for _, fe := range verrs {
		name := fe.Field()
		if name == "" {
			name = fe.StructField()
		}
		fields[name] = fieldErrorMessage(fe)
	}
	return fields
}

func fieldErrorMessage(fe validator.FieldError) string {
	isString := fe.Kind() == reflect.String || fe.Kind() == reflect.Slice || fe.Kind() == reflect.Map
	switch fe.Tag() {
	case "required":
		return "该字段为必填项，不能为空"
	case "email":
		return "邮箱格式不正确"
	case "oneof":
		return fmt.Sprintf("取值只能为：%s", fe.Param())
	case "min":
		if isString {
			return fmt.Sprintf("长度不能少于 %s 个字符", fe.Param())
		}
		return fmt.Sprintf("数值不能小于 %s", fe.Param())
	case "max":
		if isString {
			return fmt.Sprintf("长度不能超过 %s 个字符", fe.Param())
		}
		return fmt.Sprintf("数值不能大于 %s", fe.Param())
	default:
		return "参数不合法"
	}
}

// HandleBindError 绑定/校验失败的统一响应出口。
func HandleBindError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		response.BadRequestValidation(c, ve.Fields)
		return
	}
	response.BadRequest(c, "参数校验失败: "+err.Error())
}
