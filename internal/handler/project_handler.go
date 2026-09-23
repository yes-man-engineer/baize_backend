// Package handler 把 HTTP 请求翻译成 service 调用，不放业务逻辑。
package handler

import (
	"errors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/yes-man-engineer/baize_backend/internal/repository"
	"github.com/yes-man-engineer/baize_backend/internal/service"
	"github.com/yes-man-engineer/baize_backend/pkg/logger"
	"github.com/yes-man-engineer/baize_backend/pkg/response"
)

type ProjectHandler struct {
	projects *service.ProjectService
}

func NewProjectHandler(p *service.ProjectService) *ProjectHandler {
	return &ProjectHandler{projects: p}
}

type startReq struct {
	Content string `json:"content"`
}

// Start POST /api/projects
// 用户发出的第一句话就是建项目。入口只有一个，是「已经有想法」还是
// 「还不知道能做什么」，交给模型从这句话里自己看，后端不做分支。
func (h *ProjectHandler) Start(c *gin.Context) {
	var req startReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求体格式不对")
		return
	}

	detail, err := h.projects.Start(c.Request.Context(), req.Content)
	if err != nil {
		fail(c, err)
		return
	}

	response.OK(c, detail)
}

// Detail GET /api/projects/:id
func (h *ProjectHandler) Detail(c *gin.Context) {
	detail, err := h.projects.Detail(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}

	response.OK(c, detail)
}

// fail 把 service 的错误翻译成 HTTP 响应。
// 只有预期内的错误原样告诉用户，其余一律打日志兜成 500——
// 内部错误直接抛给前端既没用又容易漏底。
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		response.NotFound(c, "这个项目不存在")
	case errors.Is(err, service.ErrEmptyMessage):
		response.BadRequest(c, err.Error())
	default:
		logger.Error("[fail] 接口处理失败", zap.String("path", c.FullPath()), zap.Error(err))
		response.ServerError(c, "服务开小差了，稍后再试")
	}
}
