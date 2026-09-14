package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Ollama client — talks to the local REST API directly. No SDK.
// ---------------------------------------------------------------------------

const (
	defaultOllamaURL    = "http://127.0.0.1:11434"
	ollamaGenerateRoute = "/api/generate"
	defaultVisionModel  = "minicpm-v4.6:latest"
	ollamaHTTPTimeout   = 10 * time.Minute // local LLMs are slow; vision takes a while
)

// visionModel resolves the model tag: OLLAMA_MODEL env var overrides the
// default (e.g. OLLAMA_MODEL=minicpm-v4.6:latest or any local vision model).
func visionModel() string {
	if m := strings.TrimSpace(os.Getenv("OLLAMA_MODEL")); m != "" {
		return m
	}
	return defaultVisionModel
}

// OllamaClient performs generate calls against a local Ollama daemon.
type OllamaClient struct {
	BaseURL string
	HTTP    *http.Client
	Model   string
}

// NewOllamaClient returns a client pointed at the local daemon.
func NewOllamaClient() *OllamaClient {
	return &OllamaClient{
		BaseURL: defaultOllamaURL,
		Model:   visionModel(),
		HTTP:    &http.Client{Timeout: ollamaHTTPTimeout},
	}
}

// ollamaGenerateRequest is the wire format for /api/generate.
type ollamaGenerateRequest struct {
	Model  string   `json:"model"`
	Prompt string   `json:"prompt"`
	Images []string `json:"images,omitempty"` // base64 (no data: prefix)
	Stream bool     `json:"stream"`
	// Options nudges the model toward a deterministic, structured answer.
	Options map[string]any `json:"options,omitempty"`
}

// ollamaGenerateResponse is the (stream=false) reply.
type ollamaGenerateResponse struct {
	Response      string `json:"response"`
	Done          bool   `json:"done"`
	TotalDuration int64  `json:"total_duration,omitempty"`
	EvalCount     int64  `json:"eval_count,omitempty"`
}

// Ping verifies the daemon is up by listing local models.
// Returns nil if Ollama is reachable.
func (c *OllamaClient) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("ollama: build request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ollama: cannot reach %s — is `ollama serve` running? (%w)", c.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama: unexpected status %s from /api/tags", resp.Status)
	}
	return nil
}

// HasModel checks whether the vision model has been pulled locally.
func (c *OllamaClient) HasModel(ctx context.Context, model string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	if err != nil {
		return false, err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return false, fmt.Errorf("ollama: decode /api/tags: %w", err)
	}

	for _, m := range tags.Models {
		want := model
		if i := strings.IndexByte(m.Name, ':'); i > 0 {
			want = model[:i]
		}
		if m.Name == model || strings.HasPrefix(m.Name, want+":") {
			return true, nil
		}
	}
	return false, nil
}

// GenerateVision sends a prompt plus a base64 screenshot to the vision model
// and returns the full text response (non-streaming).
func (c *OllamaClient) GenerateVision(ctx context.Context, prompt string, pngBytes []byte) (string, error) {
	if len(pngBytes) == 0 {
		return "", fmt.Errorf("ollama: empty image buffer")
	}

	payload := ollamaGenerateRequest{
		Model:  c.Model,
		Prompt: prompt,
		Images: []string{base64.StdEncoding.EncodeToString(pngBytes)},
		Stream: false,
		Options: map[string]any{
			"temperature": 0.2,  // factual research notes, not poetry
			"num_ctx":     8192, // room for the image tokens + prompt
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("ollama: marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, ollamaHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+ollamaGenerateRoute, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ollama: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: generate call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("ollama: /api/generate returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out ollamaGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("ollama: decode response: %w", err)
	}
	if strings.TrimSpace(out.Response) == "" {
		return "", fmt.Errorf("ollama: model returned an empty response")
	}
	return sanitizeResponse(out.Response), nil
}

// sanitizeResponse removes reasoning-model artifacts (the <think>...</think>
// chain-of-thought block) so reports contain only the final synthesis.
func sanitizeResponse(s string) string {
	// Reasoning models emit deliberation inside <think> blocks; everything
	// after the LAST closing tag is the polished answer.
	if idx := strings.LastIndex(s, "</think>"); idx >= 0 {
		s = s[idx+len("</think>"):]
	}
	s = strings.TrimSpace(s)
	// Drop a stray unclosed opening tag if the model forgot to close it.
	s = strings.TrimPrefix(s, "<think>")
	return strings.TrimSpace(s)
}
