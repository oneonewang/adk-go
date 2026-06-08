// Package main demonstrates a minimal OpenAI-compatible LLM adapter for ADK.
//
// The adapter implements google.golang.org/adk/model.LLM by translating
// *model.LLMRequest into a POST to {OPENAI_BASE_URL}/chat/completions and
// parsing the JSON response back into *model.LLMResponse. It works against
// any server that speaks the OpenAI Chat Completions protocol — OpenAI,
// DeepSeek, Moonshot, Ollama, vLLM, etc.
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

	"google.golang.org/adk/model"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-4o-mini"
)

// openaiAdapter is the model.LLM implementation backed by the OpenAI Chat
// Completions HTTP API.
type openaiAdapter struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// New constructs an adapter. baseURL or modelName may be empty; defaults are
// applied. apiKey may be empty only for endpoints that do not require auth
// (e.g. a local Ollama server).
func New(apiKey, baseURL, modelName string) model.LLM {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if modelName == "" {
		modelName = defaultModel
	}
	return &openaiAdapter{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   modelName,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Name returns the configured model identifier.
func (o *openaiAdapter) Name() string { return o.model }

// GenerateContent implements model.LLM. The stream parameter is accepted
// for interface compliance but the minimal adapter always issues a
// non-streaming request and yields a single complete response.
func (o *openaiAdapter) GenerateContent(ctx context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if req == nil || len(req.Contents) == 0 {
			yield(nil, errors.New("openaiAdapter: empty request"))
			return
		}
		if !hasUserContent(req.Contents) {
			yield(nil, errors.New("openaiAdapter: request must include a user message"))
			return
		}

		body, err := json.Marshal(buildRequest(o.model, req.Contents))
		if err != nil {
			yield(nil, fmt.Errorf("marshal request: %w", err))
			return
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			o.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			yield(nil, fmt.Errorf("new request: %w", err))
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if o.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
		}

		resp, err := o.client.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("call upstream: %w", err))
			return
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			yield(nil, fmt.Errorf("read body: %w", err))
			return
		}
		if resp.StatusCode/100 != 2 {
			yield(nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, string(raw)))
			return
		}
		out, err := parseResponse(raw)
		if err != nil {
			yield(nil, fmt.Errorf("parse response: %w", err))
			return
		}
		yield(out, nil)
	}
}

// ---------- 协议转换 ----------

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiRequest struct {
	Model    string       `json:"model"`
	Messages []oaiMessage `json:"messages"`
}

type oaiChoice struct {
	Message oaiMessage `json:"message"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
}

// buildRequest maps genai.Content slices into OpenAI Chat Completions
// messages. ADK roles are "user" and "model"; OpenAI uses "user" and
// "assistant", so "model" is rewritten on the way out.
func buildRequest(modelName string, contents []*genai.Content) oaiRequest {
	msgs := make([]oaiMessage, 0, len(contents))
	for _, c := range contents {
		if c == nil {
			continue
		}
		var sb strings.Builder
		for _, p := range c.Parts {
			if p == nil || p.Text == "" {
				continue
			}
			sb.WriteString(p.Text)
		}
		role := c.Role
		switch role {
		case genai.RoleModel:
			role = "assistant"
		case "":
			role = "user"
		}
		msgs = append(msgs, oaiMessage{Role: role, Content: sb.String()})
	}
	return oaiRequest{Model: modelName, Messages: msgs}
}

// parseResponse turns the first choice's content into a non-partial
// LLMResponse. Streaming and tool-call decoding are intentionally omitted.
func parseResponse(raw []byte) (*model.LLMResponse, error) {
	var cr oaiResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return nil, err
	}
	if len(cr.Choices) == 0 {
		return nil, errors.New("openaiAdapter: empty choices")
	}
	return &model.LLMResponse{
		Content:      genai.NewContentFromText(cr.Choices[0].Message.Content, genai.RoleModel),
		Partial:      false,
		TurnComplete: true,
	}, nil
}

// hasUserContent reports whether any *genai.Content in the slice carries a
// user-role turn with non-empty text. OpenAI-compatible endpoints reject
// prompts that are missing a user turn.
func hasUserContent(contents []*genai.Content) bool {
	for _, c := range contents {
		if c == nil || c.Role != genai.RoleUser {
			continue
		}
		for _, p := range c.Parts {
			if p != nil && p.Text != "" {
				return true
			}
		}
	}
	return false
}

// ---------- 入口 ----------

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	modelName := os.Getenv("OPENAI_MODEL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if modelName == "" {
		modelName = defaultModel
	}
	if apiKey == "" && !strings.Contains(baseURL, "localhost") && !strings.Contains(baseURL, "127.0.0.1") {
		fmt.Fprintln(os.Stderr, "warning: OPENAI_API_KEY is empty (required for non-local endpoints)")
	}
	llm := New(apiKey, baseURL, modelName)
	fmt.Printf("openaiAdapter ready (model=%s, baseURL=%s)\n", llm.Name(), baseURL)
}
