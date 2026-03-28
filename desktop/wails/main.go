//go:build desktop

package main

import (
	"context"
	"embed"
	"log/slog"
	"net/http"
	"os"
	"time"

	"gopher-ai/internal/api"
	"gopher-ai/internal/attachments"
	"gopher-ai/internal/config"
	"gopher-ai/internal/gemini"
	"gopher-ai/internal/inference"
	"gopher-ai/internal/llama"
	"gopher-ai/internal/storage"
	"gopher-ai/internal/training"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed frontend/dist
var assets embed.FS

type App struct {
	backendURL string
	logger     *slog.Logger
	server     *http.Server
	llama      *llama.Client
	training   *training.Service
}

func NewApp() *App {
	return &App{
		backendURL: "http://127.0.0.1:38080",
		logger:     slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
}

func (a *App) startup(ctx context.Context) {
	go func() {
		if err := a.startBackend(); err != nil {
			a.logger.Error("desktop backend failed", "err", err)
		}
	}()
}

func (a *App) shutdown(ctx context.Context) {
	if a.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = a.server.Shutdown(shutdownCtx)
	}
	if a.llama != nil {
		_ = a.llama.Close()
	}
	if a.training != nil {
		a.training.Close()
	}
}

func (a *App) BackendURL() string {
	return a.backendURL
}

func (a *App) startBackend() error {
	cfg, err := config.Load("")
	if err != nil {
		return err
	}
	cfg.HTTP.Address = "127.0.0.1:38080"
	if err := cfg.EnsureDirectories(); err != nil {
		return err
	}

	quotaTracker, err := gemini.NewRateLimiter(cfg.Paths.QuotaTrackingFile, gemini.Limits{
		DailyQuota:   cfg.Gemini.DailyQuota,
		RequestLimit: cfg.Gemini.RequestLimitDaily,
		Cooldown:     time.Duration(cfg.Gemini.RequestCooldownSeconds) * time.Second,
	})
	if err != nil {
		return err
	}

	var geminiClient *gemini.Client
	if cfg.APIKeys.Gemini != "" {
		geminiClient = gemini.NewClient(cfg.APIKeys.Gemini, cfg.Gemini.BaseURL, quotaTracker)
	}

	a.llama = llama.NewClient(cfg.Llama, a.logger)
	chatStore := storage.NewChatStore(cfg.Paths.ChatsDir, cfg.Paths.TrashDir)
	attachmentService := attachments.NewService(cfg.Paths.AttachmentsTempDir, cfg.Paths.AttachmentsMetaDir, cfg.Features.AttachmentMaxSize)
	inferenceService := inference.NewService(cfg, a.llama, geminiClient, a.logger)
	a.training = training.NewService(cfg, chatStore, a.llama, a.logger)
	server := api.NewServer(cfg, a.logger, chatStore, attachmentService, inferenceService, a.training, a.llama, quotaTracker, nil)

	a.server = &http.Server{
		Addr:              cfg.HTTP.Address,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	a.logger.Info("starting desktop backend", "addr", cfg.HTTP.Address)
	return a.server.ListenAndServe()
}

func main() {
	app := NewApp()
	if err := wails.Run(&options.App{
		Title:     "Gopher AI",
		Width:     1480,
		Height:    920,
		MinWidth:  1180,
		MinHeight: 760,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Bind: []any{app},
		OnStartup: func(ctx context.Context) {
			app.startup(ctx)
		},
		OnShutdown: func(ctx context.Context) {
			app.shutdown(ctx)
		},
	}); err != nil {
		panic(err)
	}
}
