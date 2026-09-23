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
	interview *service.InterviewService
	path      *service.PathService
	plan      *service.PlanService
}

func NewProjectHandler(i *service.InterviewService, pa *service.PathService, pl *service.PlanService) *ProjectHandler {
	return &ProjectHandler{interview: i, path: pa, plan: pl}
}

type createReq struct {
	// Idea 可以为空。为空就是入口 B：不知道能做什么，先进盘点。
	Idea string `json:"idea"`
}

// Create POST /api/projects
func (h *ProjectHandler) Create(c *gin.Context) {
	var req createReq
	if err := c.ShouldBindJSON(&req); err != nil && c.Request.ContentLength > 0 {
		response.BadRequest(c, "请求体格式不对")
		return
	}

	p, err := h.interview.Start(c.Request.Context(), req.Idea)
	if err != nil {
		fail(c, err)
		return
	}

	// 开场白要等模型十几秒，不放在这里。前端拿到 token 先跳转，再去要开场白。
	response.OK(c, gin.H{
		"project":     p,
		"next_action": service.ActionOpening,
	})
}

// Opening POST /api/projects/:token/opening
func (h *ProjectHandler) Opening(c *gin.Context) {
	p, question, next, err := h.interview.Opening(c.Request.Context(), c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}

	response.OK(c, gin.H{
		"project":     p,
		"question":    question,
		"next_action": next,
	})
}

type answerReq struct {
	Content string `json:"content" binding:"required"`
}

// Answer POST /api/projects/:token/answers
func (h *ProjectHandler) Answer(c *gin.Context) {
	var req answerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "content 不能为空")
		return
	}

	p, question, next, err := h.interview.Answer(c.Request.Context(), c.Param("token"), req.Content)
	if err != nil {
		fail(c, err)
		return
	}

	response.OK(c, gin.H{
		"project":  p,
		"question": question,
		// next_action: ask=继续答题，paths=去生成候选路径，plan=去生成方案
		"next_action": next,
		"done":        next != service.ActionAsk,
	})
}

// GeneratePaths POST /api/projects/:token/paths
// 入口 B：盘点完给 3 条能启动的路。用户点「够了」也走这里。
func (h *ProjectHandler) GeneratePaths(c *gin.Context) {
	p, paths, err := h.path.Generate(c.Request.Context(), c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, gin.H{
		"project": p,
		"paths":   paths,
	})
}

// SelectPath POST /api/projects/:token/paths/:id/select
// 选一条，然后立刻合流回主干提问。
func (h *ProjectHandler) SelectPath(c *gin.Context) {
	p, question, next, err := h.path.Select(c.Request.Context(), c.Param("token"), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, gin.H{
		"project":     p,
		"question":    question,
		"next_action": next,
		"done":        next != service.ActionAsk,
	})
}

// GeneratePlan POST /api/projects/:token/plan
// 用户点「够了，先给我方案」也走这里。
func (h *ProjectHandler) GeneratePlan(c *gin.Context) {
	detail, err := h.plan.Generate(c.Request.Context(), c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, detail)
}

// Detail GET /api/projects/:token
func (h *ProjectHandler) Detail(c *gin.Context) {
	detail, err := h.plan.Detail(c.Request.Context(), c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, detail)
}

type verifyReq struct {
	Answer string `json:"answer" binding:"required"`
}

// Verify POST /api/projects/:token/items/:id/verify
// 回填真实数据，这一条从黄/红变绿。
func (h *ProjectHandler) Verify(c *gin.Context) {
	var req verifyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "answer 不能为空")
		return
	}

	item, err := h.plan.Verify(c.Request.Context(), c.Param("token"), c.Param("id"), req.Answer)
	if err != nil {
		fail(c, err)
		return
	}

	detail, err := h.plan.Detail(c.Request.Context(), c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}

	response.OK(c, gin.H{
		"item":     item,
		"progress": detail.Progress,
	})
}

// End POST /api/projects/:token/end
func (h *ProjectHandler) End(c *gin.Context) {
	p, err := h.plan.End(c.Request.Context(), c.Param("token"))
	if err != nil {
		fail(c, err)
		return
	}
	response.OK(c, gin.H{"project": p})
}

func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		response.NotFound(c, "项目不存在或链接失效")
	case errors.Is(err, service.ErrProjectEnded):
		response.Conflict(c, "项目已结束")
	case errors.Is(err, service.ErrNotAnswering):
		response.Conflict(c, "当前阶段不需要回答问题")
	case errors.Is(err, service.ErrNotScouting):
		response.Conflict(c, "当前阶段不能生成候选路径")
	case errors.Is(err, service.ErrNotChoosing):
		response.Conflict(c, "当前阶段不能选择路径")
	case errors.Is(err, service.ErrPathsIncomplete):
		// 模型这次没凑够 3 条，重试一般就好了。
		response.Conflict(c, "这次没能给齐 3 条路，再试一次")
	case errors.Is(err, service.ErrMustChoosePath):
		response.Conflict(c, "请先从候选路径里选一条")
	case errors.Is(err, service.ErrEmptyAnswer):
		response.BadRequest(c, "内容不能为空")
	case errors.Is(err, service.ErrNoConversation):
		response.BadRequest(c, "还没聊过，先回答几个问题")
	default:
		logger.Error("handler error", zap.Error(err), zap.String("path", c.Request.URL.Path))
		response.ServerError(c, "服务开小差了，稍后再试")
	}
}
