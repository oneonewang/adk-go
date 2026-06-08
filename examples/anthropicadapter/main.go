// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package anthropicadapter is a minimal [model.LLM] adapter for Anthropic's
// Claude Messages API. Read alongside docs/tutorials/05-llm-providers/04-anthropic.md.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model"
)

// Config 描述一个 Anthropic 端点的全部参数。
type Config struct {
	APIKey     string // ANTHROPIC_API_KEY（"sk-ant-" 前缀）
	Model      string // 默认 claude-3-5-sonnet-20241022
	BaseURL    string // 默认 https://api.anthropic.com
	MaxTokens  int    // Anthropic 强制要求
	HTTPClient *http.Client
}

// anthropicModel 是 model.LLM 的最小实现：只发 HTTP，不引入 SDK。
type anthropicModel struct {
	httpClient *http.Client
	apiKey     string
	modelName  string
	baseURL    string
	maxTokens  int
}

// NewModel 构造一个 *anthropicModel 并以 model.LLM 形式返回。
func NewModel(ctx context.Context, cfg Config) (model.LLM, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("anthropicadapter: APIKey is required")
	}
	if cfg.Model == "" {
		cfg.Model = "claude-3-5-sonnet-20241022"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1024
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	_ = ctx
	return &anthropicModel{
		httpClient: cfg.HTTPClient, apiKey: cfg.APIKey,
		modelName: cfg.Model, baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		maxTokens: cfg.MaxTokens,
	}, nil
}

// Name 返回构造时指定的默认模型名。
func (m *anthropicModel) Name() string { return m.modelName }

// GenerateContent 把 req.Contents 翻译成 Anthropic messages 格式、HTTP POST
// 到 {baseURL}/v1/messages，并把响应体翻译回 *model.LLMResponse。
func (m *anthropicModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		_ = stream // 同步实现；流式留作升级
		if req == nil {
			yield(nil, errors.New("anthropicadapter: nil request"))
			return
		}
		modelName := m.modelName
		if req.Model != "" {
			modelName = req.Model
		}
		system, msgs := splitContents(req.Contents)
		body, err := json.Marshal(anthropicRequest{
			Model: modelName, System: system, Messages: msgs, MaxTokens: m.maxTokens,
		})
		if err != nil {
			yield(nil, fmt.Errorf("marshal request: %w", err))
			return
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			m.baseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			yield(nil, fmt.Errorf("new request: %w", err))
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", m.apiKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		resp, err := m.httpClient.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("call upstream: %w", err))
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			b, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(b)))
			return
		}
		out, err := parseAnthropicResponse(resp.Body)
		if err != nil {
			yield(nil, fmt.Errorf("parse response: %w", err))
			return
		}
		yield(out, nil)
	}
}

// ---------- 协议结构体 ----------

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	MaxTokens int                `json:"max_tokens"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicResponse struct {
	ID           string         `json:"id"`
	Content      []contentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	Model        string         `json:"model"`
	InputTokens  int            `json:"input_tokens"`
	OutputTokens int            `json:"output_tokens"`
}

// splitContents 把 ADK 的 []*genai.Content 拆成 (system, messages)：
// 1) 第一条 system role 拼成顶层 system 字符串；
// 2) 其余 user / assistant 按顺序追加，合并相邻同角色消息（API 不允许连续同角色）。
func splitContents(contents []*genai.Content) (string, []anthropicMessage) {
	var system string
	var msgs []anthropicMessage
	for _, c := range contents {
		if c == nil {
			continue
		}
		switch c.Role {
		case "system":
			if system != "" {
				continue
			}
			system = joinParts(c.Parts)
		case "user", "assistant":
			text := joinParts(c.Parts)
			if len(msgs) > 0 && msgs[len(msgs)-1].Role == c.Role {
				msgs[len(msgs)-1].Content += "\n" + text
				continue
			}
			msgs = append(msgs, anthropicMessage{Role: c.Role, Content: text})
		}
	}
	return system, msgs
}

// joinParts 拼一个 *genai.Content 的所有 text part；非文本 part 被忽略。
func joinParts(parts []*genai.Part) string {
	var sb strings.Builder
	for _, p := range parts {
		if p == nil {
			continue
		}
		if p.Text != "" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// parseAnthropicResponse 把 Anthropic 同步响应翻译回 *model.LLMResponse。
// 本教程只读 content[0].text；tool_use / 多模态 / 思考块留作升级。
func parseAnthropicResponse(r io.Reader) (*model.LLMResponse, error) {
	var ar anthropicResponse
	if err := json.NewDecoder(r).Decode(&ar); err != nil {
		return nil, err
	}
	if len(ar.Content) == 0 {
		return nil, errors.New("anthropicadapter: empty content")
	}
	return &model.LLMResponse{
		Content:      genai.NewContentFromText(ar.Content[0].Text, genai.RoleModel),
		ModelVersion: ar.Model,
		Partial:      false, TurnComplete: true,
	}, nil
}

// ---------- main 入口 ----------

func main() {
	ctx := context.Background()
	const defaultModelName = "claude-3-5-sonnet-20241022"
	llm, err := NewModel(ctx, Config{
		APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
		Model:     getenv("ANTHROPIC_MODEL", defaultModelName),
		BaseURL:   getenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
		MaxTokens: 1024,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "anthropicadapter:", err)
		os.Exit(1)
	}
	a, err := llmagent.New(llmagent.Config{
		Name:        "claude_agent",
		Model:       llm,
		Description: "Agent that calls Claude via Anthropic Messages API.",
		Instruction: "Answer in one short sentence. No tools.",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "create agent:", err)
		os.Exit(1)
	}
	fmt.Printf("Using model: %s (Name() = %q)\n", llm.Name(), llm.Name())
	config := &launcher.Config{AgentLoader: agent.NewSingleLoader(a)}
	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "run failed:", err)
		fmt.Fprintln(os.Stderr, l.CommandLineSyntax())
		os.Exit(1)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
