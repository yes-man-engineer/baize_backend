package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/yes-man-engineer/baize_backend/internal/dao"
	"github.com/yes-man-engineer/baize_backend/internal/llm"
	"github.com/yes-man-engineer/baize_backend/internal/model"
	"github.com/yes-man-engineer/baize_backend/pkg/logger"
)

var ErrNotChatting = errors.New("这个项目已经聊完了")

// extractTimeout 抽取在后台跑，用户已经拿到回复走人了，不能挂太久占着连接。
const extractTimeout = 2 * time.Minute

type ReplyReq struct {
	ProjectID string `json:"project_id"`
	// Content 可以为空：表示用户没有新话要说，只要模型先开口。
	// 刚建完项目的第一次就是这样。
	Content string `json:"content"`
}

type ReplyResp struct {
	MessageID string `json:"message_id"`
}

// Reply 让模型回一句，边生成边通过 onDelta 往外推。
//
// 落库时机是有讲究的：用户这句话先只挂在内存里给模型看，等整段回复
// 成功生成之后才和回复一起入库。中途失败用户会重发，提前落库的话
// 库里就会留下两条一模一样的。
func Reply(ctx context.Context, req ReplyReq, onDelta func(text string, thinking bool)) (*ReplyResp, error) {
	project, err := dao.GetProject(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if !project.Status.Chatting() {
		return nil, ErrNotChatting
	}

	history, err := dao.ListMessages(ctx, project.ID)
	if err != nil {
		return nil, err
	}

	var incoming *model.Message
	if content := strings.TrimSpace(req.Content); content != "" {
		incoming = &model.Message{ProjectID: project.ID, Role: model.RoleUser, Content: content}
		history = append(history, *incoming)
	}

	// 生成和落库脱离请求的 context。用户关页面、刷新、切后台、断网，
	// 请求的 context 立刻就取消了，挂在上面的话这一整段回复连同他刚说的
	// 那句话会一起作废，他刷新回来什么都没有，还得重问一遍。
	// 现在他走了这边照样生成完入库，回来就能看到。
	// 超时由 llm 客户端自己的 http.Client 兜着，这里不再叠一层。
	genCtx := context.WithoutCancel(ctx)

	start := time.Now()
	var firstDelta time.Duration
	var thinkingChars int
	full, err := llm.Stream(genCtx, llm.ChatMessages(project.Facts, toLLM(history)), func(text string, thinking bool) {
		if thinking {
			thinkingChars += len([]rune(text))
		} else if firstDelta == 0 {
			firstDelta = time.Since(start)
		}
		onDelta(text, thinking)
	})
	if err != nil {
		return nil, err
	}
	// 首字延迟基本等于模型思考了多久，思考字数一起记着，这两个数要对得上。
	logger.Info("[Reply] 回复生成完成",
		zap.String("project_id", project.ID),
		zap.Duration("首字", firstDelta),
		zap.Duration("总计", time.Since(start)),
		zap.Int("思考字数", thinkingChars),
		zap.Int("字数", len([]rune(full))))

	if incoming != nil {
		if err := dao.CreateMessage(genCtx, incoming); err != nil {
			return nil, err
		}
	}

	reply := &model.Message{ProjectID: project.ID, Role: model.RoleAssistant, Content: full}
	if err := dao.CreateMessage(genCtx, reply); err != nil {
		return nil, err
	}

	extractLater(project.ID)
	return &ReplyResp{MessageID: reply.ID}, nil
}

// extractLater 回完话之后在后台整理 facts、顺便判断还要不要接着聊。
//
// 用户这时候已经拿到回复了，这一步再慢也不影响他。整理结果下一轮才用得上，
// 所以这次失败不要紧，下一轮会重新整理一遍。
//
// 这里必须自己开 context：请求的 context 在响应结束时就被取消了，
// 拿它进后台等于刚起步就被掐断。
func extractLater(projectID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), extractTimeout)
		defer cancel()

		if err := extract(ctx, projectID); err != nil {
			logger.Warn("[extractLater] 整理已确认信息失败，下一轮再试",
				zap.String("project_id", projectID), zap.Error(err))
		}
	}()
}

func extract(ctx context.Context, projectID string) error {
	project, err := dao.GetProject(ctx, projectID)
	if err != nil {
		return err
	}

	history, err := dao.ListMessages(ctx, project.ID)
	if err != nil {
		return err
	}

	var result llm.ExtractResult
	if err := llm.JSON(ctx, llm.ExtractMessages(toLLM(history)), &result); err != nil {
		return err
	}

	if len(result.Facts) > 0 {
		project.Facts = result.Facts
	}
	// 聊够了是模型的判断，改状态是代码的事。
	// 只从 chatting 往前推一格，不碰已经出过方案或者已结束的项目。
	if result.Enough && project.Status == model.StatusChatting {
		project.Status = model.StatusReady
	}

	return dao.SaveProject(ctx, project)
}

func toLLM(history []model.Message) []llm.Message {
	out := make([]llm.Message, 0, len(history))
	for _, m := range history {
		out = append(out, llm.Message{Role: llm.Role(m.Role), Content: m.Content})
	}
	return out
}
