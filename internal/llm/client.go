// Package llm 负责和大模型打交道：请求怎么发、输出怎么取回来。
//
// 只做两件事，分得很开：
//
//   - Stream 让模型说人话，纯文本逐段吐出来。面向用户，必须流式。
//   - JSON   让模型按结构吐数据。不面向用户，出错了不致命。
//
// 上一版把这两件事挤在一个方法里，说话也要求返 JSON，结果是提示词里塞满
// 格式说明，模型还时不时无视格式直接吐大白话。分开之后，说话那条路上
// 一句格式约束都不需要。
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/yes-man-engineer/baize_backend/pkg/logger"
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

// client 走 OpenAI 兼容的 /chat/completions 接口。
// DeepSeek、Kimi、通义、智谱都能用同一套，换 BaseURL 和 Model 即可。
type client struct {
	baseURL     string
	apiKey      string
	model       string
	temperature *float32
	http        *http.Client
}

var conn *client

// Init 在 main 里调一次。temperature 传 nil 表示不带这个参数，由模型用自己的默认值。
// Kimi 的 k2/k3 只接受 1，传别的值会被拒。
func Init(baseURL, apiKey, model string, temperature *float32, timeout time.Duration) {
	conn = &client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		model:       model,
		temperature: temperature,
		http:        &http.Client{Timeout: timeout},
	}
}

// Stream 让模型说话，每吐出一段就调一次 onDelta，最后返回拼完整的全文。
// onDelta 是用来往浏览器推的，返回值是用来落库的。
func Stream(ctx context.Context, msgs []Message, onDelta func(string)) (string, error) {
	return conn.streamChat(ctx, msgs, onDelta)
}

// JSON 让模型按结构吐数据，反序列化到 out。
func JSON(ctx context.Context, msgs []Message, out any) error {
	return conn.jsonChat(ctx, msgs, out)
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Stream         bool            `json:"stream,omitempty"`
	Temperature    *float32        `json:"temperature,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

func (c *client) streamChat(ctx context.Context, msgs []Message, onDelta func(string)) (string, error) {
	body := chatRequest{
		Model:       c.model,
		Messages:    msgs,
		Stream:      true,
		Temperature: c.temperature,
	}

	resp, err := c.post(ctx, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	// 单条 SSE 数据行可能很长，默认 64KB 上限不够用。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// 诊断首字延迟用的。定位完就删，不要留成长期代码。
	start := time.Now()
	var firstChunk, firstContent time.Duration
	chunks, reasoning := 0, 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
					// 先思考再回答的模型把思考过程放这里。我们原来不解析，
					// 如果它一直在发，界面上就是几十秒的空白。
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// 单个分片解析不了就跳过，不要因此中断整段回复。
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		chunks++
		if firstChunk == 0 {
			firstChunk = time.Since(start)
		}
		reasoning += len([]rune(chunk.Choices[0].Delta.ReasoningContent))

		if delta := chunk.Choices[0].Delta.Content; delta != "" {
			if firstContent == 0 {
				firstContent = time.Since(start)
			}
			full.WriteString(delta)
			onDelta(delta)
		}
	}

	logger.Info("[streamChat] 分片统计",
		zap.Duration("首个分片", firstChunk),
		zap.Duration("首个正文", firstContent),
		zap.Int("分片数", chunks),
		zap.Int("思考字数", reasoning))

	if err := scanner.Err(); err != nil {
		return full.String(), fmt.Errorf("读取模型流失败: %w", err)
	}

	if full.Len() == 0 {
		return "", fmt.Errorf("模型没有返回内容")
	}
	return full.String(), nil
}

func (c *client) jsonChat(ctx context.Context, msgs []Message, out any) error {
	body := chatRequest{
		Model:          c.model,
		Messages:       msgs,
		Temperature:    c.temperature,
		ResponseFormat: &responseFormat{Type: "json_object"},
	}

	resp, err := c.post(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var parsed struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("解析模型响应失败: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return fmt.Errorf("模型没有返回内容")
	}

	raw := parsed.Choices[0].Message.Content
	cleaned := extractJSON(raw)
	if cleaned == "" {
		return fmt.Errorf("模型返回里找不到 JSON: %s", truncate(raw, 200))
	}
	if err := json.Unmarshal([]byte(cleaned), out); err != nil {
		return fmt.Errorf("解析模型 JSON 失败: %w，原文: %s", err, truncate(cleaned, 200))
	}
	return nil
}

func (c *client) post(ctx context.Context, body chatRequest) (*http.Response, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("未配置 LLM_API_KEY")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用模型失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("模型返回 %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	return resp, nil
}

// extractJSON 容错提取：有的模型会裹一层 ```json 或者前后带解释文字。
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
