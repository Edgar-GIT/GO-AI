package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	defer llamaClient.Close()
	defer trainingService.Close()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}
