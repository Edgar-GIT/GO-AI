package llama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopher-ai/internal/config"
)

var ErrLocalServerUnavailable = errors.New("llama.cpp server unavailable")

type Client struct {
	cfg        config.LlamaConfig
	logger     *slog.Logger
	httpClient *http.Client

	mu  sync.Mutex
	cmd *exec.Cmd
}

type Status struct {
	Enabled           bool   `json:"enabled"`
	Configured        bool   `json:"configured"`
	Reachable         bool   `json:"reachable"`
	AutoStart         bool   `json:"autoStart"`
	ServerURL         string `json:"serverUrl"`
	BinaryPath        string `json:"binaryPath"`
	ModelPath         string `json:"modelPath"`
	ModelAlias        string `json:"modelAlias"`
	ActiveAdapterPath string `json:"activeAdapterPath,omitempty"`
	ContextSize       int    `json:"contextSize"`
	MaxTokens         int    `json:"maxTokens"`
	GPULayers         int    `json:"gpuLayers"`
	FlashAttention    bool   `json:"flashAttention"`
	Status            string `json:"status"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   Usage                  `json:"usage"`
}

type ChatCompletionChoice struct {
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewClient(cfg config.LlamaConfig, logger *slog.Logger) *Client {
	return &Client{
		cfg:    cfg,
		logger: logger,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

func (c *Client) Status(ctx context.Context) Status {
	status := Status{
		Enabled:           c.cfg.Enabled,
		Configured:        strings.TrimSpace(c.cfg.ServerURL) != "",
		AutoStart:         c.cfg.AutoStart,
		ServerURL:         c.cfg.ServerURL,
		BinaryPath:        c.cfg.BinaryPath,
		ModelPath:         c.cfg.ModelPath,
		ModelAlias:        c.cfg.ModelAlias,
		ActiveAdapterPath: c.cfg.ActiveAdapterPath,
		ContextSize:       c.cfg.ContextSize,
		MaxTokens:         c.cfg.MaxTokens,
		GPULayers:         c.cfg.GPULayers,
		FlashAttention:    c.cfg.FlashAttention,
		Status:            "disabled",
	}

	if !c.cfg.Enabled {
		return status
	}
	if strings.TrimSpace(c.cfg.ServerURL) == "" {
		status.Status = "not_configured"
		return status
	}

	health, err := c.Health(ctx)
	if err != nil {
		status.Status = "unreachable"
		return status
	}

	status.Reachable = strings.EqualFold(health.Status, "ok")
	if status.Reachable {
		status.Status = "ready"
	}

	return status
}

func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.cfg.ServerURL, "/")+"/health", nil)
	if err != nil {
		return HealthResponse{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return HealthResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return HealthResponse{}, fmt.Errorf("%w: status %d %s", ErrLocalServerUnavailable, resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var parsed HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return HealthResponse{}, err
	}

	return parsed, nil
}

func (c *Client) GenerateChatCompletion(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error) {
	if !c.cfg.Enabled {
		return ChatCompletionResponse{}, ErrLocalServerUnavailable
	}
	if err := c.EnsureRunning(ctx); err != nil {
		return ChatCompletionResponse{}, err
	}

	if strings.TrimSpace(req.Model) == "" {
		req.Model = c.cfg.ModelAlias
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = c.cfg.MaxTokens
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ChatCompletionResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.ServerURL, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(c.cfg.APIToken); token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ChatCompletionResponse{}, fmt.Errorf("%w: status %d %s", ErrLocalServerUnavailable, resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var parsed ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ChatCompletionResponse{}, err
	}

	return parsed, nil
}

func (c *Client) EnsureRunning(ctx context.Context) error {
	if !c.cfg.Enabled {
		return ErrLocalServerUnavailable
	}

	healthCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	_, err := c.Health(healthCtx)
	cancel()
	if err == nil {
		return nil
	}

	if !c.cfg.AutoStart {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd != nil && c.cmd.Process != nil && c.cmd.ProcessState == nil {
		return c.waitUntilHealthy(ctx)
	}

	if strings.TrimSpace(c.cfg.BinaryPath) == "" || strings.TrimSpace(c.cfg.ModelPath) == "" {
		return fmt.Errorf("%w: missing llama binary or model path", ErrLocalServerUnavailable)
	}

	cmd, err := c.buildCommand()
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	c.cmd = cmd
	if c.logger != nil {
		c.logger.Info("started llama-server", "server_url", c.cfg.ServerURL, "model_path", c.cfg.ModelPath)
	}

	go func() {
		waitErr := cmd.Wait()
		c.mu.Lock()
		c.cmd = nil
		c.mu.Unlock()
		if waitErr != nil && c.logger != nil {
			c.logger.Warn("llama-server exited", "err", waitErr)
		}
	}()

	return c.waitUntilHealthy(ctx)
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}

	err := c.cmd.Process.Kill()
	c.cmd = nil
	return err
}

func (c *Client) SetActiveAdapterPath(ctx context.Context, adapterPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	adapterPath = strings.TrimSpace(adapterPath)
	if adapterPath != "" {
		if _, err := os.Stat(adapterPath); err != nil {
			return err
		}
	}

	c.cfg.ActiveAdapterPath = adapterPath

	if c.cmd == nil || c.cmd.Process == nil || c.cmd.ProcessState != nil {
		if c.cfg.AutoStart {
			return c.waitUntilRestarted(ctx)
		}
		return nil
	}

	if err := c.cmd.Process.Kill(); err != nil {
		return err
	}
	c.cmd = nil
	return c.waitUntilRestarted(ctx)
}

func (c *Client) ActiveAdapterPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.ActiveAdapterPath
}

func (r ChatCompletionResponse) Text() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(r.Choices[0].Message.Content)
}

func (c *Client) buildCommand() (*exec.Cmd, error) {
	parsedURL, err := url.Parse(c.cfg.ServerURL)
	if err != nil {
		return nil, err
	}

	host := parsedURL.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}

	port := parsedURL.Port()
	if port == "" {
		port = "8081"
	}

	args := []string{
		"-m", c.cfg.ModelPath,
		"--host", host,
		"--port", port,
		"--alias", c.cfg.ModelAlias,
		"-c", strconv.Itoa(c.cfg.ContextSize),
		"--n-gpu-layers", strconv.Itoa(c.cfg.GPULayers),
	}
	if c.cfg.FlashAttention {
		args = append(args, "--flash-attn")
	}
	if adapterPath := strings.TrimSpace(c.cfg.ActiveAdapterPath); adapterPath != "" {
		args = append(args, "--lora", adapterPath)
	}
	if token := strings.TrimSpace(c.cfg.APIToken); token != "" {
		args = append(args, "--api-key", token)
	}

	return exec.Command(c.cfg.BinaryPath, args...), nil
}

func (c *Client) waitUntilHealthy(ctx context.Context) error {
	deadline := time.NewTimer(25 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("%w: startup timed out", ErrLocalServerUnavailable)
		case <-ticker.C:
			healthCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			_, err := c.Health(healthCtx)
			cancel()
			if err == nil {
				return nil
			}
		}
	}
}

func (c *Client) waitUntilRestarted(ctx context.Context) error {
	if !c.cfg.AutoStart {
		return nil
	}
	c.mu.Unlock()
	err := c.EnsureRunning(ctx)
	c.mu.Lock()
	return err
}
