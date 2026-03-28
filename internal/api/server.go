package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopher-ai/internal/attachments"
	"gopher-ai/internal/chat"
	"gopher-ai/internal/config"
	"gopher-ai/internal/gemini"
	"gopher-ai/internal/inference"
	"gopher-ai/internal/llama"
	"gopher-ai/internal/storage"
	"gopher-ai/internal/training"
)

type Server struct {
	mu          sync.RWMutex
	cfg         config.AppConfig
	logger      *slog.Logger
	chatStore   *storage.ChatStore
	attachments *attachments.Service
	inference   *inference.Service
	training    *training.Service
	llama       *llama.Client
	quota       *gemini.RateLimiter
	staticFS    fs.FS
	mux         *http.ServeMux
}

func NewServer(
	cfg config.AppConfig,
	logger *slog.Logger,
	chatStore *storage.ChatStore,
	attachmentService *attachments.Service,
	inferenceService *inference.Service,
	trainingService *training.Service,
	llamaClient *llama.Client,
	quotaTracker *gemini.RateLimiter,
	staticFS fs.FS,
) *Server {
	server := &Server{
		cfg:         cfg,
		logger:      logger,
		chatStore:   chatStore,
		attachments: attachmentService,
		inference:   inferenceService,
		training:    trainingService,
		llama:       llamaClient,
		quota:       quotaTracker,
		staticFS:    staticFS,
		mux:         http.NewServeMux(),
	}
	server.registerRoutes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.withRecover(s.withLogging(s.withCORS(s.mux)))
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /api/app/bootstrap", s.handleBootstrap)
	s.mux.HandleFunc("GET /api/system/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/system/quota", s.handleQuota)
	s.mux.HandleFunc("POST /api/system/quota/reset", s.handleQuotaReset)
	s.mux.HandleFunc("POST /api/system/settings", s.handleSystemSettings)
	s.mux.HandleFunc("GET /api/models", s.handleModels)
	s.mux.HandleFunc("GET /api/training/status", s.handleTrainingStatus)
	s.mux.HandleFunc("POST /api/training/manual", s.handleTrainingManual)
	s.mux.HandleFunc("GET /api/training/tasks/{taskID}/dataset", s.handleTrainingDataset)
	s.mux.HandleFunc("POST /api/training/tasks/{taskID}/apply", s.handleTrainingApply)

	s.mux.HandleFunc("POST /api/chats", s.handleCreateChat)
	s.mux.HandleFunc("GET /api/chats", s.handleListChats)
	s.mux.HandleFunc("GET /api/chats/{chatID}", s.handleGetChat)
	s.mux.HandleFunc("PUT /api/chats/{chatID}", s.handleUpdateChat)
	s.mux.HandleFunc("DELETE /api/chats/{chatID}", s.handleDeleteChat)
	s.mux.HandleFunc("POST /api/chats/{chatID}/messages", s.handleSendMessage)

	s.mux.HandleFunc("POST /api/attachments/upload", s.handleUploadAttachment)
	s.mux.HandleFunc("GET /api/attachments/{attachmentID}", s.handleGetAttachment)
	s.mux.HandleFunc("DELETE /api/attachments/{attachmentID}", s.handleDeleteAttachment)

	s.mux.Handle("/", s.handleStatic())
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	cfg := s.currentConfig()

	chats, err := s.chatStore.List(r.Context(), storage.ListOptions{
		Limit: 20,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"username": cfg.Username,
			"theme":    cfg.Theme,
		},
		"models": s.inference.Snapshot(r.Context()),
		"quota":  s.quota.Snapshot(),
		"chats":  chats,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	cfg := s.currentConfig()
	quota := s.quota.Snapshot()
	snapshot := s.inference.Snapshot(r.Context())
	geminiStatus := "requires_api_key"
	if snapshot.GeminiConfigured {
		geminiStatus = "ready"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC(),
		"models": map[string]any{
			"local":  snapshot.Local.Status,
			"gemini": geminiStatus,
		},
		"storage": map[string]any{
			"root": cfg.Paths.Root,
		},
		"quota": quota,
	})
}

func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.quota.Snapshot())
}

func (s *Server) handleQuotaReset(w http.ResponseWriter, r *http.Request) {
	if err := s.quota.Reset(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, s.quota.Snapshot())
}

func (s *Server) handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.currentConfig()
	previousGeminiKey := cfg.APIKeys.Gemini

	var req struct {
		APIKeys *struct {
			Gemini *string `json:"gemini"`
		} `json:"apiKeys"`
		Models *struct {
			Primary *string `json:"primary"`
		} `json:"models"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.APIKeys != nil && req.APIKeys.Gemini != nil {
		cfg.APIKeys.Gemini = strings.TrimSpace(*req.APIKeys.Gemini)
	}

	if req.Models != nil && req.Models.Primary != nil {
		value := strings.TrimSpace(*req.Models.Primary)
		if value != "" {
			cfg.Models.Primary = value
		}
	}

	if err := cfg.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if req.APIKeys != nil && req.APIKeys.Gemini != nil && strings.TrimSpace(previousGeminiKey) != cfg.APIKeys.Gemini {
		if err := s.quota.Reset(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	var geminiClient *gemini.Client
	if cfg.APIKeys.Gemini != "" {
		geminiClient = gemini.NewClient(cfg.APIKeys.Gemini, cfg.Gemini.BaseURL, s.quota)
	}

	s.setConfig(cfg)
	s.inference.Reconfigure(cfg, geminiClient)

	writeJSON(w, http.StatusOK, map[string]any{
		"saved":  true,
		"models": s.inference.Snapshot(r.Context()),
		"quota":  s.quota.Snapshot(),
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.inference.Snapshot(r.Context()))
}

func (s *Server) handleTrainingStatus(w http.ResponseWriter, r *http.Request) {
	queue, err := s.training.Status()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, queue)
}

func (s *Server) handleTrainingManual(w http.ResponseWriter, r *http.Request) {
	var req training.ManualRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	task, err := s.training.EnqueueManual(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleTrainingDataset(w http.ResponseWriter, r *http.Request) {
	path, err := s.training.DatasetPath(r.PathValue("taskID"))
	if err != nil {
		switch {
		case errors.Is(err, training.ErrTaskNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, os.ErrNotExist):
			writeError(w, http.StatusNotFound, "dataset not prepared for task")
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	http.ServeFile(w, r, path)
}

func (s *Server) handleTrainingApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AdapterPath string `json:"adapterPath"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	adapterPath := strings.TrimSpace(req.AdapterPath)
	if adapterPath == "" {
		adapterPath = s.training.DefaultAdapterPath(r.PathValue("taskID"))
	}
	if _, err := os.Stat(adapterPath); err != nil {
		writeError(w, http.StatusBadRequest, "adapter file not found")
		return
	}

	completed, err := s.training.ApplyResult(r.PathValue("taskID"), adapterPath)
	if err != nil {
		if errors.Is(err, training.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cfg := s.currentConfig()
	cfg.Llama.ActiveAdapterPath = adapterPath
	if err := cfg.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setConfig(cfg)
	if s.llama != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := s.llama.SetActiveAdapterPath(ctx, adapterPath); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, completed)
}

func (s *Server) handleCreateChat(w http.ResponseWriter, r *http.Request) {
	cfg := s.currentConfig()

	var req struct {
		Title string `json:"title"`
		Model string `json:"model"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = cfg.Models.Primary
	}

	value := chat.New(req.Title, model)
	if err := s.chatStore.Create(r.Context(), value); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, value)
}

func (s *Server) handleListChats(w http.ResponseWriter, r *http.Request) {
	items, err := s.chatStore.List(r.Context(), storage.ListOptions{
		Limit:  intQuery(r, "limit", 20),
		Offset: intQuery(r, "offset", 0),
		Search: r.URL.Query().Get("search"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"limit":  intQuery(r, "limit", 20),
		"offset": intQuery(r, "offset", 0),
	})
}

func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	value, err := s.chatStore.Get(r.Context(), r.PathValue("chatID"))
	if err != nil {
		if errors.Is(err, storage.ErrChatNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, value)
}

func (s *Server) handleUpdateChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     *string           `json:"title"`
		ModelUsed *string           `json:"modelUsed"`
		Memory    *chat.MemoryState `json:"memory"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	value, err := s.chatStore.Get(r.Context(), r.PathValue("chatID"))
	if err != nil {
		if errors.Is(err, storage.ErrChatNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			title = "New Chat"
		}
		value.Title = title
	}
	if req.ModelUsed != nil {
		value.ModelUsed = strings.TrimSpace(*req.ModelUsed)
	}
	if req.Memory != nil {
		value.Memory = *req.Memory
	}
	value.UpdatedAt = time.Now().UTC()
	value.RefreshMetadata()

	if err := s.chatStore.Update(r.Context(), value); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, value)
}

func (s *Server) handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	hardDelete := strings.EqualFold(r.URL.Query().Get("hardDelete"), "true")
	if err := s.chatStore.Delete(r.Context(), r.PathValue("chatID"), hardDelete); err != nil {
		if errors.Is(err, storage.ErrChatNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted":    true,
		"hardDelete": hardDelete,
	})
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content       string   `json:"content"`
		AttachmentIDs []string `json:"attachmentIds"`
		ForceModel    string   `json:"forceModel"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if strings.TrimSpace(req.Content) == "" && len(req.AttachmentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "message content or attachments are required")
		return
	}

	conversation, err := s.chatStore.Get(r.Context(), r.PathValue("chatID"))
	if err != nil {
		if errors.Is(err, storage.ErrChatNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	attachmentRefs, err := s.loadAttachments(req.AttachmentIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	userMessage := chat.NewMessage("user", req.Content, attachmentRefs)
	if conversation.Title == "New Chat" && strings.TrimSpace(req.Content) != "" {
		conversation.Title = chat.AutoTitleFromContent(req.Content)
	}
	conversation.AddMessage(userMessage)

	if err := s.chatStore.Update(r.Context(), conversation); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result, err := s.inference.GenerateAssistantReply(r.Context(), conversation, req.ForceModel)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, inference.ErrNoInferenceProvider) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, err.Error())
		return
	}

	conversation.AddMessage(result.Message)
	if err := s.chatStore.Update(r.Context(), conversation); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var trainingTask *training.Task
	if queuedTask, queued, err := s.training.MaybeEnqueueAuto(conversation); err != nil {
		if s.logger != nil {
			s.logger.Warn("auto training enqueue failed", "chat_id", conversation.ID, "err", err)
		}
	} else if queued {
		trainingTask = &queuedTask
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"chat":             conversation,
		"userMessage":      userMessage,
		"assistantMessage": result.Message,
		"modelUsed":        result.ResolvedModel,
		"fallbackUsed":     result.FallbackUsed,
		"tokensUsed":       result.Message.TokensUsed,
		"latency":          result.Message.LatencyMS,
		"trainingTask":     trainingTask,
	})
}

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(s.cfg.Features.AttachmentMaxSize + (1 << 20)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	header, err := firstFileHeader(r.MultipartForm)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	attachment, err := s.attachments.SaveUpload(r.Context(), header)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, attachment)
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, err := s.attachments.Get(r.PathValue("attachmentID"))
	if err != nil {
		if errors.Is(err, attachments.ErrAttachmentNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", attachment.MimeType)
	http.ServeFile(w, r, attachment.LocalPath)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	if err := s.attachments.Delete(r.PathValue("attachmentID")); err != nil {
		if errors.Is(err, attachments.ErrAttachmentNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) loadAttachments(ids []string) ([]chat.AttachmentRef, error) {
	attachmentsOut := make([]chat.AttachmentRef, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		attachment, err := s.attachments.Get(id)
		if err != nil {
			return nil, err
		}
		attachmentsOut = append(attachmentsOut, attachment)
	}
	return attachmentsOut, nil
}

func (s *Server) handleStatic() http.Handler {
	staticDir := filepath.Join("web")
	if s.staticFS != nil {
		fileServer := http.FileServer(http.FS(s.staticFS))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			if r.URL.Path == "/" {
				http.ServeFileFS(w, r, s.staticFS, "index.html")
				return
			}
			cleanPath := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
			if _, err := fs.Stat(s.staticFS, cleanPath); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
			http.ServeFileFS(w, r, s.staticFS, "index.html")
		})
	}

	fileServer := http.FileServer(http.Dir(staticDir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		path := filepath.Join(staticDir, filepath.Clean(strings.TrimPrefix(r.URL.Path, "/")))
		if r.URL.Path == "/" {
			http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
			return
		}

		if _, err := os.Stat(path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
	})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		s.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("panic recovered", "panic", recovered)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func intQuery(r *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func firstFileHeader(form *multipart.Form) (*multipart.FileHeader, error) {
	if form == nil || len(form.File) == 0 {
		return nil, errors.New("missing file field")
	}

	for _, files := range form.File {
		if len(files) > 0 {
			return files[0], nil
		}
	}

	return nil, errors.New("missing file field")
}

func (s *Server) currentConfig() config.AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Server) setConfig(cfg config.AppConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}
