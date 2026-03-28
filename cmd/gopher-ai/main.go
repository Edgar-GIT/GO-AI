package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"gopher-ai/internal/api"
	"gopher-ai/internal/attachments"
	"gopher-ai/internal/config"
	"gopher-ai/internal/gemini"
	"gopher-ai/internal/inference"
	"gopher-ai/internal/llama"
	"gopher-ai/internal/storage"
	"gopher-ai/internal/training"
	webui "gopher-ai/web"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load("")
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}

	if err := cfg.EnsureDirectories(); err != nil {
		logger.Error("prepare data directories", "err", err)
		os.Exit(1)
	}

	quotaTracker, err := gemini.NewRateLimiter(cfg.Paths.QuotaTrackingFile, gemini.Limits{
		DailyQuota:   cfg.Gemini.DailyQuota,
		RequestLimit: cfg.Gemini.RequestLimitDaily,
		Cooldown:     time.Duration(cfg.Gemini.RequestCooldownSeconds) * time.Second,
	})
	if err != nil {
		logger.Error("initialize quota tracker", "err", err)
		os.Exit(1)
	}

	var geminiClient *gemini.Client
	if cfg.APIKeys.Gemini != "" {
		geminiClient = gemini.NewClient(cfg.APIKeys.Gemini, cfg.Gemini.BaseURL, quotaTracker)
	} else {
		logger.Info("Gemini API key not configured; remote inference is disabled until GEMINI_API_KEY or config.json is set")
	}

	llamaClient := llama.NewClient(cfg.Llama, logger)

	chatStore := storage.NewChatStore(cfg.Paths.ChatsDir, cfg.Paths.TrashDir)
	attachmentService := attachments.NewService(cfg.Paths.AttachmentsTempDir, cfg.Paths.AttachmentsMetaDir, cfg.Features.AttachmentMaxSize)
	inferenceService := inference.NewService(cfg, llamaClient, geminiClient, logger)
	trainingService := training.NewService(cfg, chatStore, llamaClient, logger)
	defer llamaClient.Close()
	defer trainingService.Close()

	server := api.NewServer(cfg, logger, chatStore, attachmentService, inferenceService, trainingService, llamaClient, quotaTracker, webui.Assets)

	httpServer := &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("gopher ai backend listening", "addr", cfg.HTTP.Address, "data_dir", cfg.Paths.Root)
		serverErr <- httpServer.ListenAndServe()
	}()

	uiURL := uiURLFromAddress(cfg.HTTP.Address)
	logger.Info("gopher ai ui available", "url", uiURL)
	if browserAutoOpenEnabled() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			if err := waitForHTTPReady(ctx, uiURL+"/api/system/health"); err != nil {
				logger.Warn("gopher ai ui did not become ready for browser launch", "url", uiURL, "err", err)
				return
			}
			if err := openBrowser(uiURL); err != nil {
				logger.Warn("could not open browser automatically", "url", uiURL, "err", err)
				return
			}
			logger.Info("opened gopher ai ui", "url", uiURL)
		}()
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "err", err)
			os.Exit(1)
		}
		return
	case <-signalCtx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.HTTP.ShutdownTimeoutMS)*time.Millisecond)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}

func browserAutoOpenEnabled() bool {
	value := strings.TrimSpace(os.Getenv("GOPHER_AI_NO_BROWSER"))
	return value == "" || value == "0" || strings.EqualFold(value, "false")
}

func uiURLFromAddress(address string) string {
	value := strings.TrimSpace(address)
	if value == "" {
		return "http://127.0.0.1:8080"
	}
	if strings.HasPrefix(value, ":") {
		return "http://127.0.0.1" + value
	}

	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return "http://" + value
	}

	host = strings.Trim(host, "[]")
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}

	return "http://" + net.JoinHostPort(host, port)
}

func waitForHTTPReady(ctx context.Context, target string) error {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode < http.StatusInternalServerError {
					return nil
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func openBrowser(target string) error {
	name, args, err := browserCommand(target)
	if err != nil {
		return err
	}
	return exec.Command(name, args...).Start()
}

func browserCommand(target string) (string, []string, error) {
	switch runtime.GOOS {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}, nil
	case "darwin":
		return "open", []string{target}, nil
	case "linux":
		return "xdg-open", []string{target}, nil
	default:
		return "", nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}
