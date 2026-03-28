package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"gopher-ai/internal/ids"
)

type AppConfig struct {
	Username  string          `json:"username"`
	Theme     string          `json:"theme"`
	DeviceID  string          `json:"deviceId"`
	APIKeys   APIKeysConfig   `json:"apiKeys"`
	HTTP      HTTPConfig      `json:"http"`
	Models    ModelsConfig    `json:"models"`
	Inference InferenceConfig `json:"inference"`
	Llama     LlamaConfig     `json:"llama"`
	Training  TrainingConfig  `json:"training"`
	Features  FeaturesConfig  `json:"features"`
	Gemini    GeminiConfig    `json:"gemini"`
	Mobile    MobileConfig    `json:"mobile"`
	Paths     PathsConfig     `json:"-"`
}

type APIKeysConfig struct {
	Gemini      string `json:"gemini,omitempty"`
	HuggingFace string `json:"huggingface,omitempty"`
}

type HTTPConfig struct {
	Address           string `json:"address"`
	ShutdownTimeoutMS int    `json:"shutdownTimeout"`
}

type ModelsConfig struct {
	Primary           string `json:"primary"`
	Fallback          string `json:"fallback"`
	FallbackSecondary string `json:"fallback_secondary"`
}

type InferenceConfig struct {
	UseGPU         bool   `json:"useGpu"`
	GPUType        string `json:"gpuType"`
	MaxLocalTokens int    `json:"maxLocalTokens"`
	LocalTimeoutMS int    `json:"localTimeout"`
	Quantization   string `json:"quantization"`
}

type LlamaConfig struct {
	Enabled           bool   `json:"enabled"`
	ServerURL         string `json:"serverUrl"`
	AutoStart         bool   `json:"autoStart"`
	BinaryPath        string `json:"binaryPath"`
	ModelPath         string `json:"modelPath"`
	ModelAlias        string `json:"modelAlias"`
	ActiveAdapterPath string `json:"activeAdapterPath,omitempty"`
	ContextSize       int    `json:"contextSize"`
	MaxTokens         int    `json:"maxTokens"`
	GPULayers         int    `json:"gpuLayers"`
	FlashAttention    bool   `json:"flashAttention"`
	APIToken          string `json:"apiToken,omitempty"`
}

type TrainingConfig struct {
	Enabled                bool   `json:"enabled"`
	AutoTrainingEnabled    bool   `json:"autoTrainingEnabled"`
	AutoRun                bool   `json:"autoRun"`
	AutoApplyAdapters      bool   `json:"autoApplyAdapters"`
	PrepareDatasetOnManual bool   `json:"prepareDatasetOnManual"`
	MinPairsToTrain        int    `json:"minPairsToTrain"`
	PollIntervalSeconds    int    `json:"pollIntervalSeconds"`
	PythonBinary           string `json:"pythonBinary"`
	ScriptPath             string `json:"scriptPath"`
	BaseModelName          string `json:"baseModelName"`
	DefaultEpochs          int    `json:"defaultEpochs"`
	DefaultBatchSize       int    `json:"defaultBatchSize"`
	DefaultLearningRate    string `json:"defaultLearningRate"`
}

type FeaturesConfig struct {
	MemoryEnabled        bool  `json:"memoryEnabled"`
	AutoTraining         bool  `json:"autoTraining"`
	OfflineQueue         bool  `json:"offlineQueue"`
	CacheGeminiResponses bool  `json:"cacheGeminiResponses"`
	AttachmentMaxSize    int64 `json:"attachmentMaxSize"`
}

type GeminiConfig struct {
	BaseURL                string `json:"baseUrl"`
	RateLimitTracking      bool   `json:"rateLimitTracking"`
	DailyQuota             int    `json:"dailyQuota"`
	RequestLimitDaily      int    `json:"requestLimit"`
	RequestCooldownSeconds int    `json:"requestCooldown"`
}

type MobileConfig struct {
	LastServerAddress   string `json:"lastServerAddress"`
	LastConnectTime     string `json:"lastConnectTime"`
	OfflineQueueEnabled bool   `json:"offlineQueueEnabled"`
}

type PathsConfig struct {
	Root                string
	ConfigFile          string
	QuotaTrackingFile   string
	ChatsDir            string
	TrashDir            string
	TrainingDir         string
	TrainingQueueFile   string
	TrainingDatasetsDir string
	TrainingAdaptersDir string
	TrainingLogsDir     string
	CacheDir            string
	AttachmentsDir      string
	AttachmentsTempDir  string
	AttachmentsMetaDir  string
	LogsDir             string
}

func Load(rootOverride string) (AppConfig, error) {
	root, err := resolveRoot(rootOverride)
	if err != nil {
		return AppConfig{}, err
	}

	cfg := Default(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return AppConfig{}, err
	}

	_, statErr := os.Stat(cfg.Paths.ConfigFile)
	switch {
	case statErr == nil:
		file, err := os.Open(cfg.Paths.ConfigFile)
		if err != nil {
			return AppConfig{}, err
		}
		defer file.Close()

		if err := json.NewDecoder(file).Decode(&cfg); err != nil {
			return AppConfig{}, err
		}
	case errors.Is(statErr, os.ErrNotExist):
		if err := cfg.Save(); err != nil {
			return AppConfig{}, err
		}
	default:
		return AppConfig{}, statErr
	}

	cfg.Paths = derivePaths(root)

	if cfg.Models.Primary == "" || cfg.Models.Primary == "local-llama" {
		cfg.Models.Primary = "gopher-ai"
	}
	if cfg.Models.Fallback == "" {
		cfg.Models.Fallback = "gemini-3.1-pro-preview"
	}
	if cfg.Models.FallbackSecondary == "" {
		cfg.Models.FallbackSecondary = "gemini-3-flash-preview"
	}

	if value := os.Getenv("GEMINI_API_KEY"); value != "" {
		cfg.APIKeys.Gemini = value
	}

	return cfg, nil
}

func Default(root string) AppConfig {
	return AppConfig{
		Username: "Edgar",
		Theme:    "sleepy-dark-blue-aquarium",
		DeviceID: ids.New("device"),
		APIKeys:  APIKeysConfig{},
		HTTP: HTTPConfig{
			Address:           ":8080",
			ShutdownTimeoutMS: 10_000,
		},
		Models: ModelsConfig{
			Primary:           "gopher-ai",
			Fallback:          "gemini-3.1-pro-preview",
			FallbackSecondary: "gemini-3-flash-preview",
		},
		Inference: InferenceConfig{
			UseGPU:         true,
			GPUType:        "auto",
			MaxLocalTokens: 2048,
			LocalTimeoutMS: 30_000,
			Quantization:   "q4_k_m",
		},
		Llama: LlamaConfig{
			Enabled:        true,
			ServerURL:      "http://127.0.0.1:8081",
			AutoStart:      false,
			BinaryPath:     "llama-server",
			ModelPath:      filepath.Join(root, "cache", "local_models", "model.gguf"),
			ModelAlias:     "local-llama",
			ContextSize:    4096,
			MaxTokens:      512,
			GPULayers:      -1,
			FlashAttention: true,
		},
		Training: TrainingConfig{
			Enabled:                true,
			AutoTrainingEnabled:    true,
			AutoRun:                true,
			AutoApplyAdapters:      true,
			PrepareDatasetOnManual: true,
			MinPairsToTrain:        10,
			PollIntervalSeconds:    3,
			PythonBinary:           "python3",
			ScriptPath:             "scripts/train_lora.py",
			BaseModelName:          "Mistral-7B-Instruct-v0.3",
			DefaultEpochs:          3,
			DefaultBatchSize:       8,
			DefaultLearningRate:    "0.0001",
		},
		Features: FeaturesConfig{
			MemoryEnabled:        true,
			AutoTraining:         true,
			OfflineQueue:         true,
			CacheGeminiResponses: true,
			AttachmentMaxSize:    50 * 1024 * 1024,
		},
		Gemini: GeminiConfig{
			BaseURL:                "https://generativelanguage.googleapis.com/v1beta",
			RateLimitTracking:      true,
			DailyQuota:             15_000,
			RequestLimitDaily:      100,
			RequestCooldownSeconds: 60,
		},
		Mobile: MobileConfig{
			OfflineQueueEnabled: true,
		},
		Paths: derivePaths(root),
	}
}

func (cfg AppConfig) EnsureDirectories() error {
	dirs := []string{
		cfg.Paths.Root,
		cfg.Paths.ChatsDir,
		cfg.Paths.TrashDir,
		cfg.Paths.TrainingDir,
		cfg.Paths.TrainingDatasetsDir,
		cfg.Paths.TrainingAdaptersDir,
		cfg.Paths.TrainingLogsDir,
		cfg.Paths.CacheDir,
		cfg.Paths.AttachmentsDir,
		cfg.Paths.AttachmentsTempDir,
		cfg.Paths.AttachmentsMetaDir,
		cfg.Paths.LogsDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return nil
}

func (cfg AppConfig) Save() error {
	if err := os.MkdirAll(filepath.Dir(cfg.Paths.ConfigFile), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	tempPath := cfg.Paths.ConfigFile + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		return err
	}

	return os.Rename(tempPath, cfg.Paths.ConfigFile)
}

func resolveRoot(rootOverride string) (string, error) {
	if rootOverride != "" {
		return filepath.Clean(rootOverride), nil
	}

	if value := os.Getenv("GOPHER_AI_HOME"); value != "" {
		return filepath.Clean(value), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".gopher-ai"), nil
}

func derivePaths(root string) PathsConfig {
	return PathsConfig{
		Root:                root,
		ConfigFile:          filepath.Join(root, "config.json"),
		QuotaTrackingFile:   filepath.Join(root, "quota_tracking.json"),
		ChatsDir:            filepath.Join(root, "chats"),
		TrashDir:            filepath.Join(root, "trash"),
		TrainingDir:         filepath.Join(root, "training"),
		TrainingQueueFile:   filepath.Join(root, "training", "training_queue.json"),
		TrainingDatasetsDir: filepath.Join(root, "training", "datasets"),
		TrainingAdaptersDir: filepath.Join(root, "training", "adapters"),
		TrainingLogsDir:     filepath.Join(root, "training", "logs"),
		CacheDir:            filepath.Join(root, "cache"),
		AttachmentsDir:      filepath.Join(root, "attachments"),
		AttachmentsTempDir:  filepath.Join(root, "attachments", "temp"),
		AttachmentsMetaDir:  filepath.Join(root, "attachments", "meta"),
		LogsDir:             filepath.Join(root, "logs"),
	}
}
