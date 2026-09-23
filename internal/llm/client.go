package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/yes-man-engineer/baize_backend/pkg/logger"
)

// errBadJSON 模型没按 response_format 返回 JSON。只有这种错值得重试，
// 超时、网络失败、额度不足再要一遍也是一样的结果，白等一轮。
var errBadJSON = errors.New("模型返回的不是 JSON")

// finishReasonStop 模型自己写完了。其他值都意味着输出没写全。
const finishReasonStop = "stop"

// chatJSONAttempts 含首次在内的总次数。
const chatJSONAttempts = 3

// jsonRepairPrompt 重试时追加。原样重发没有意义：模型是被 prompt 稳定诱导
// 才吐的大白话，同样的输入只会得到同样的输出，必须明确告诉它上一次错在哪。
const jsonRepairPrompt = "你刚才的回复不是 JSON，已被丢弃。" +
	"重新回答一次，整个回复必须是一个 JSON 对象，以 { 开头、以 } 结尾，" +
	"不要有任何额外文字。你要说的话放进 question 字段里。"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Client 走 OpenAI 兼容的 /chat/completions 接口。
// DeepSeek、Kimi、通义、智谱都能用同一套，换 BaseURL 和 Model 即可。
type Client struct {
	baseURL     string
	apiKey      string
	model       string
	temperature *float32
	http        *http.Client
}

// NewClient 的 temperature 传 nil 表示不带这个参数，由模型用自己的默认值。
func NewClient(baseURL, apiKey, model string, temperature *float32, timeout time.Duration) *Client {
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		model:       model,
		temperature: temperature,
		http:        &http.Client{Timeout: timeout},
	}
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    *float32        `json:"temperature,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
		// FinishReason "stop" 是模型自己写完了，"length" 是撞上 token 上限被截断。
		// 不看这个的话，截断会伪装成"模型只给了 4 条"，方向全错。
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ChatJSON 让模型返回 JSON 并反序列化到 out，格式不对会重试。
func (c *Client) ChatJSON(ctx context.Context, msgs []Message, out any) error {
	var err error
	for attempt := 1; attempt <= chatJSONAttempts; attempt++ {
		attemptMsgs := msgs
		if attempt > 1 {
			attemptMsgs = append(append([]Message{}, msgs...),
				Message{Role: RoleUser, Content: jsonRepairPrompt})
		}

		err = c.chatJSONOnce(ctx, attemptMsgs, out)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errBadJSON) {
			return err
		}
		logger.Warn("[ChatJSON] 模型没按 JSON 返回，重试",
			zap.Int("attempt", attempt), zap.Error(err))
	}
	return err
}

func (c *Client) chatJSONOnce(ctx context.Context, msgs []Message, out any) error {
	raw, err := c.chat(ctx, msgs, true)
	if err != nil {
		return err
	}

	cleaned := extractJSON(raw)
	if cleaned == "" {
		return fmt.Errorf("%w，原文: %s", errBadJSON, truncate(raw, 300))
	}
	if err := json.Unmarshal([]byte(cleaned), out); err != nil {
		return fmt.Errorf("%w: %v，原文: %s", errBadJSON, err, truncate(cleaned, 300))
	}
	return nil
}

func (c *Client) chat(ctx context.Context, msgs []Message, wantJSON bool) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("未配置 LLM_API_KEY")
	}

	body := chatRequest{
		Model:       c.model,
		Messages:    msgs,
		Temperature: c.temperature,
	}
	if wantJSON {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("调用模型失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("模型返回 %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("解析模型响应失败: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("模型报错: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("模型没有返回内容")
	}

	if reason := parsed.Choices[0].FinishReason; reason != "" && reason != finishReasonStop {
		logger.Warn("[chat] 模型不是正常结束",
			zap.String("finish_reason", reason),
			zap.Int("completion_tokens", parsed.Usage.CompletionTokens))
	}

	return parsed.Choices[0].Message.Content, nil
}

// extractJSON 容错提取 JSON。
// 有的模型不支持 response_format，会裹一层 ```json 或者前后带解释文字。
func extractJSON(s string) string {
	s = strings.TrimSpace(s)

	if idx := strings.Index(s, "```"); idx >= 0 {
		rest := s[idx+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			rest = rest[:end]
		}
		s = strings.TrimSpace(rest)
	}

	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
