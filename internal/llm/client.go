package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

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
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func NewClient(baseURL, apiKey, model string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: timeout},
	}
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    float32         `json:"temperature"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ChatJSON 让模型返回 JSON 并反序列化到 out。
func (c *Client) ChatJSON(ctx context.Context, msgs []Message, out any) error {
	raw, err := c.chat(ctx, msgs, true)
	if err != nil {
		return err
	}

	cleaned := extractJSON(raw)
	if cleaned == "" {
		return fmt.Errorf("模型返回里找不到 JSON: %s", truncate(raw, 300))
	}
	if err := json.Unmarshal([]byte(cleaned), out); err != nil {
		return fmt.Errorf("解析模型 JSON 失败: %w，原文: %s", err, truncate(cleaned, 300))
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
		Temperature: 0.4,
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
