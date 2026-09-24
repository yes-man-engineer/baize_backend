// Package handler 只做一件事：把 gin 和业务函数接起来。
//
// 绑参数、翻译错误、写响应都收在这里，业务函数里看不到 *gin.Context，
// 它的入参是业务请求，出参是业务响应，换个框架这一层重写就行。
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/yes-man-engineer/baize_backend/internal/dao"
	"github.com/yes-man-engineer/baize_backend/internal/service"
	"github.com/yes-man-engineer/baize_backend/pkg/logger"
	"github.com/yes-man-engineer/baize_backend/pkg/response"
)

// Decorate 接一次性返回的业务函数。
func Decorate[Req, Resp any](biz func(context.Context, Req) (Resp, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		req, err := bind[Req](c)
		if err != nil {
			response.BadRequest(c, "请求参数不对")
			return
		}

		resp, err := biz(c.Request.Context(), req)
		if err != nil {
			fail(c, err)
			return
		}

		response.OK(c, resp)
	}
}

// DecorateNoResp 接只返回 error 的业务函数，成功时 data 是 null。
// 删除、结束这类接口没什么好返回的，不用为了凑签名编一个空结构体出来。
func DecorateNoResp[Req any](biz func(context.Context, Req) error) gin.HandlerFunc {
	return Decorate(func(ctx context.Context, req Req) (any, error) {
		return nil, biz(ctx, req)
	})
}

// DecorateStream 接边算边吐的业务函数，返回 SSE 流。
// 业务函数拿到的 onDelta 只管往外推文本，推到哪去是这一层的事。
func DecorateStream[Req, Resp any](biz func(context.Context, Req, func(string)) (Resp, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		req, err := bind[Req](c)
		if err != nil {
			response.BadRequest(c, "请求参数不对")
			return
		}

		send := openStream(c)

		resp, err := biz(c.Request.Context(), req, func(delta string) {
			send(sseEvent{Type: "delta", Text: delta})
		})
		if err != nil {
			// 头已经发出去了，改不了 HTTP 状态码，错误只能当成一个事件推下去。
			send(sseEvent{Type: "error", Msg: streamErrorMessage(c, err)})
			return
		}

		send(sseEvent{Type: "done", Data: resp})
	}
}

// 推给前端的事件。type 决定前端怎么处理这一条。
type sseEvent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Data any    `json:"data,omitempty"`
	Msg  string `json:"message,omitempty"`
}

// bind 路径参数和请求体绑到同一个结构体上，业务结构体只需要 json 标签。
//
// 路径参数**放在最后**绑：它是权威的，请求体里塞一个同名字段也盖不掉，
// 否则谁都能拿自己的 URL 去读别人的项目。
func bind[Req any](c *gin.Context) (Req, error) {
	var req Req
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			return req, err
		}
	}
	bindURI(c, &req)
	return req, nil
}

// bindURI 按 json 标签名去找同名的路径参数，所以路由里的占位符要和标签同名，
// 比如 json:"project_id" 对应 /projects/:project_id。
//
// gin 自己的 ShouldBindUri 只认 uri 标签，用它就得在业务结构体上留框架痕迹，
// 这十几行是拿来换掉那个标签的。只支持 string 字段，我们的 id 都是 string。
func bindURI(c *gin.Context, req any) {
	v := reflect.ValueOf(req).Elem()
	if v.Kind() != reflect.Struct {
		return
	}

	fields := v.Type()
	for i := 0; i < fields.NumField(); i++ {
		name, _, _ := strings.Cut(fields.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}

		raw := c.Param(name)
		if raw == "" {
			continue
		}

		field := v.Field(i)
		if field.Kind() == reflect.String && field.CanSet() {
			field.SetString(raw)
		}
	}
}

// openStream 把响应切成 SSE，返回往外推事件的函数。
func openStream(c *gin.Context) func(sseEvent) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	// 告诉 nginx 这条响应不要缓冲。不加的话 nginx 会攒齐了再一次性吐给
	// 浏览器，前后端都写对了也看不到流式效果。
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(200)
	c.Writer.Flush()

	return func(e sseEvent) {
		payload, err := json.Marshal(e)
		if err != nil {
			return
		}
		c.Writer.Write([]byte("data: "))
		c.Writer.Write(payload)
		c.Writer.Write([]byte("\n\n"))
		c.Writer.Flush()
	}
}

// fail 把业务错误翻译成 HTTP 响应。
// 只有预期内的错误原样告诉用户，其余一律打日志兜成 500。
// 内部错误直接抛给前端既没用又容易漏底。
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, dao.ErrNotFound):
		response.NotFound(c, "这个项目不存在")
	case errors.Is(err, service.ErrEmptyMessage):
		response.BadRequest(c, err.Error())
	default:
		logger.Error("[fail] 接口处理失败", zap.String("path", c.FullPath()), zap.Error(err))
		response.ServerError(c, "服务开小差了，稍后再试")
	}
}

func streamErrorMessage(c *gin.Context, err error) string {
	switch {
	case errors.Is(err, dao.ErrNotFound):
		return "这个项目不存在"
	case errors.Is(err, service.ErrNotChatting):
		return err.Error()
	default:
		logger.Error("[streamErrorMessage] 生成回复失败",
			zap.String("path", c.FullPath()), zap.Error(err))
		return "服务开小差了，稍后再试"
	}
}
