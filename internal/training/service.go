package training

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopher-ai/internal/chat"
	"gopher-ai/internal/config"
	"gopher-ai/internal/ids"
	"gopher-ai/internal/llama"
	"gopher-ai/internal/storage"
)

var ErrTaskNotFound = errors.New("training task not found")

type Service struct {
	cfg       config.AppConfig
	chatStore *storage.ChatStore
	llama     *llama.Client
	logger    *slog.Logger
	mu        sync.Mutex
	wakeCh    chan struct{}
	cancel    context.CancelFunc
	doneCh    chan struct{}
}

type Queue struct {
	AutoTrainingEnabled bool            `json:"autoTrainingEnabled"`
	PendingTasks        []Task          `json:"pendingTasks"`
	CompletedTasks      []CompletedTask `json:"completedTasks"`
}

type Task struct {
	TaskID        string        `json:"taskId"`
	Status        string        `json:"status"`
	Type          string        `json:"type"`
	CreatedAt     time.Time     `json:"createdAt"`
	StartedAt     *time.Time    `json:"startedAt,omitempty"`
	CompletedAt   *time.Time    `json:"completedAt,omitempty"`
	ChatIDs       []string      `json:"chatIds"`
	MessageCount  int           `json:"messageCount"`
	DatasetPath   string        `json:"datasetPath,omitempty"`
	BaseModel     string        `json:"baseModel,omitempty"`
	TargetMetrics TargetMetrics `json:"targetMetrics"`
	ManualCommand string        `json:"manualCommand,omitempty"`
	LogPath       string        `json:"logPath,omitempty"`
	Result        *Result       `json:"result,omitempty"`
	LastError     string        `json:"lastError,omitempty"`
}

type TargetMetrics struct {
	Epochs       int     `json:"epochs"`
	LearningRate float64 `json:"learningRate"`
	BatchSize    int     `json:"batchSize"`
}

type Result struct {
	AdapterID      string  `json:"adapterId"`
	Accuracy       float64 `json:"accuracy,omitempty"`
	Loss           float64 `json:"loss,omitempty"`
	CheckpointPath string  `json:"checkpointPath,omitempty"`
}

type CompletedTask struct {
	TaskID       string    `json:"taskId"`
	Type         string    `json:"type"`
	CompletedAt  time.Time `json:"completedAt"`
	ChatIDs      []string  `json:"chatIds,omitempty"`
	MessageCount int       `json:"messageCount,omitempty"`
	BaseModel    string    `json:"baseModel,omitempty"`
	AdapterID    string    `json:"adapterId,omitempty"`
	AdapterPath  string    `json:"adapterPath,omitempty"`
	AutoApplied  bool      `json:"autoApplied"`
	ApplyError   string    `json:"applyError,omitempty"`
	Accuracy     float64   `json:"accuracy,omitempty"`
	Loss         float64   `json:"loss,omitempty"`
	DatasetPath  string    `json:"datasetPath,omitempty"`
	LogPath      string    `json:"logPath,omitempty"`
}

type ManualRequest struct {
	ChatIDs      []string `json:"chatIds"`
	Epochs       int      `json:"epochs"`
	LearningRate float64  `json:"learningRate"`
	BatchSize    int      `json:"batchSize"`
	BaseModel    string   `json:"baseModel"`
}

type DatasetRow struct {
	Instruction  string `json:"instruction"`
	Response     string `json:"response"`
	ChatID       string `json:"chatId"`
	UserMessage  string `json:"userMessageId"`
	ReplyMessage string `json:"replyMessageId"`
}

type runnerOutput struct {
	Status      string  `json:"status"`
	Message     string  `json:"message"`
	Manifest    string  `json:"manifest"`
	Rows        int     `json:"rows"`
	OutputDir   string  `json:"outputDir"`
	AdapterPath string  `json:"adapterPath"`
	Accuracy    float64 `json:"accuracy"`
	Loss        float64 `json:"loss"`
}

func NewService(cfg config.AppConfig, chatStore *storage.ChatStore, llamaClient *llama.Client, logger *slog.Logger) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		cfg:       cfg,
		chatStore: chatStore,
		llama:     llamaClient,
		logger:    logger,
		wakeCh:    make(chan struct{}, 1),
		cancel:    cancel,
		doneCh:    make(chan struct{}),
	}
	go service.run(ctx)
	service.notify()
	return service
}

func (s *Service) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	<-s.doneCh
}

func (s *Service) Status() (Queue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadQueueLocked()
}

func (s *Service) EnqueueManual(ctx context.Context, req ManualRequest) (Task, error) {
	if len(req.ChatIDs) == 0 {
		return Task{}, errors.New("at least one chatId is required")
	}

	rows, err := s.collectDataset(ctx, req.ChatIDs)
	if err != nil {
		return Task{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	queue, err := s.loadQueueLocked()
	if err != nil {
		return Task{}, err
	}

	task, err := s.newTaskLocked("manual", req.ChatIDs, req.BaseModel, req.Epochs, req.LearningRate, req.BatchSize, rows)
	if err != nil {
		return Task{}, err
	}

	queue.PendingTasks = append(queue.PendingTasks, task)
	if err := s.saveQueueLocked(queue); err != nil {
		return Task{}, err
	}

	if task.Status == "queued" {
		s.notify()
	}

	return task, nil
}

func (s *Service) MaybeEnqueueAuto(conversation chat.Chat) (Task, bool, error) {
	if !s.cfg.Training.Enabled || !s.cfg.Features.AutoTraining || !s.cfg.Training.AutoTrainingEnabled {
		return Task{}, false, nil
	}

	rows := rowsFromChat(conversation)
	if len(rows) < s.minPairsToTrain() {
		return Task{}, false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	queue, err := s.loadQueueLocked()
	if err != nil {
		return Task{}, false, err
	}

	if s.hasPendingTaskForChatLocked(queue, conversation.ID) {
		return Task{}, false, nil
	}

	lastTrainedPairs := s.lastCompletedPairsForChatLocked(queue, conversation.ID)
	if len(rows)-lastTrainedPairs < s.minPairsToTrain() {
		return Task{}, false, nil
	}

	task, err := s.newTaskLocked("auto", []string{conversation.ID}, "", 0, 0, 0, rows)
	if err != nil {
		return Task{}, false, err
	}

	queue.PendingTasks = append(queue.PendingTasks, task)
	if err := s.saveQueueLocked(queue); err != nil {
		return Task{}, false, err
	}

	if task.Status == "queued" {
		s.notify()
	}

	return task, true, nil
}

func (s *Service) DatasetPath(taskID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	queue, err := s.loadQueueLocked()
	if err != nil {
		return "", err
	}

	for _, task := range queue.PendingTasks {
		if task.TaskID == taskID {
			if strings.TrimSpace(task.DatasetPath) == "" {
				return "", os.ErrNotExist
			}
			return task.DatasetPath, nil
		}
	}

	for _, task := range queue.CompletedTasks {
		if task.TaskID == taskID {
			if strings.TrimSpace(task.DatasetPath) == "" {
				return "", os.ErrNotExist
			}
			return task.DatasetPath, nil
		}
	}

	return "", ErrTaskNotFound
}

func (s *Service) DefaultAdapterPath(taskID string) string {
	return filepath.Join(s.cfg.Paths.TrainingAdaptersDir, taskID, "adapter.safetensors")
}

func (s *Service) ApplyResult(taskID, adapterPath string) (CompletedTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	queue, err := s.loadQueueLocked()
	if err != nil {
		return CompletedTask{}, err
	}

	for i := range queue.CompletedTasks {
		if queue.CompletedTasks[i].TaskID == taskID {
			queue.CompletedTasks[i].AdapterPath = adapterPath
			queue.CompletedTasks[i].AutoApplied = true
			queue.CompletedTasks[i].ApplyError = ""
			if err := s.saveQueueLocked(queue); err != nil {
				return CompletedTask{}, err
			}
			return queue.CompletedTasks[i], nil
		}
	}

	index := -1
	var task Task
	for i, candidate := range queue.PendingTasks {
		if candidate.TaskID == taskID {
			index = i
			task = candidate
			break
		}
	}
	if index == -1 {
		return CompletedTask{}, ErrTaskNotFound
	}

	completed := CompletedTask{
		TaskID:       task.TaskID,
		Type:         task.Type,
		CompletedAt:  time.Now().UTC(),
		ChatIDs:      append([]string(nil), task.ChatIDs...),
		MessageCount: task.MessageCount,
		BaseModel:    task.BaseModel,
		AdapterID:    task.TaskID,
		AdapterPath:  adapterPath,
		AutoApplied:  true,
		DatasetPath:  task.DatasetPath,
		LogPath:      task.LogPath,
	}
	if task.Result != nil {
		completed.Accuracy = task.Result.Accuracy
		completed.Loss = task.Result.Loss
	}

	queue.PendingTasks = append(queue.PendingTasks[:index], queue.PendingTasks[index+1:]...)
	queue.CompletedTasks = append(queue.CompletedTasks, completed)
	if err := s.saveQueueLocked(queue); err != nil {
		return CompletedTask{}, err
	}

	return completed, nil
}

func (s *Service) run(ctx context.Context) {
	defer close(s.doneCh)

	interval := time.Duration(s.cfg.Training.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 3 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wakeCh:
		}

		for {
			processed, err := s.processNext(ctx)
			if err != nil && s.logger != nil {
				s.logger.Error("training task failed", "err", err)
			}
			if !processed {
				break
			}
		}
	}
}

func (s *Service) processNext(ctx context.Context) (bool, error) {
	if !s.cfg.Training.Enabled || !s.cfg.Training.AutoRun {
		return false, nil
	}

	task, ok, err := s.claimNextTask()
	if err != nil || !ok {
		return ok, err
	}

	completed, runErr := s.runTask(ctx, task)
	if finishErr := s.finishTask(task.TaskID, completed, runErr); finishErr != nil {
		return true, finishErr
	}
	return true, runErr
}

func (s *Service) claimNextTask() (Task, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	queue, err := s.loadQueueLocked()
	if err != nil {
		return Task{}, false, err
	}

	for i := range queue.PendingTasks {
		if queue.PendingTasks[i].Status != "queued" && queue.PendingTasks[i].Status != "prepared" {
			continue
		}
		now := time.Now().UTC()
		queue.PendingTasks[i].Status = "processing"
		queue.PendingTasks[i].StartedAt = &now
		queue.PendingTasks[i].CompletedAt = nil
		queue.PendingTasks[i].LastError = ""
		queue.PendingTasks[i].LogPath = s.defaultLogPath(queue.PendingTasks[i].TaskID)
		if err := s.saveQueueLocked(queue); err != nil {
			return Task{}, false, err
		}
		return queue.PendingTasks[i], true, nil
	}

	return Task{}, false, nil
}

func (s *Service) finishTask(taskID string, completed CompletedTask, runErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	queue, err := s.loadQueueLocked()
	if err != nil {
		return err
	}

	index := -1
	for i := range queue.PendingTasks {
		if queue.PendingTasks[i].TaskID == taskID {
			index = i
			break
		}
	}
	if index == -1 {
		return ErrTaskNotFound
	}

	now := time.Now().UTC()
	queue.PendingTasks[index].CompletedAt = &now

	if runErr != nil {
		queue.PendingTasks[index].Status = "failed"
		queue.PendingTasks[index].LastError = runErr.Error()
		return s.saveQueueLocked(queue)
	}

	queue.PendingTasks = append(queue.PendingTasks[:index], queue.PendingTasks[index+1:]...)
	queue.CompletedTasks = append(queue.CompletedTasks, completed)
	return s.saveQueueLocked(queue)
}

func (s *Service) runTask(ctx context.Context, task Task) (CompletedTask, error) {
	if err := os.MkdirAll(s.cfg.Paths.TrainingLogsDir, 0o755); err != nil {
		return CompletedTask{}, err
	}
	if err := os.MkdirAll(filepath.Join(s.cfg.Paths.TrainingAdaptersDir, task.TaskID), 0o755); err != nil {
		return CompletedTask{}, err
	}

	scriptPath, err := s.resolveScriptPath()
	if err != nil {
		return CompletedTask{}, err
	}

	logPath := task.LogPath
	if strings.TrimSpace(logPath) == "" {
		logPath = s.defaultLogPath(task.TaskID)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return CompletedTask{}, err
	}
	defer logFile.Close()

	outputDir := filepath.Join(s.cfg.Paths.TrainingAdaptersDir, task.TaskID)
	args := []string{
		scriptPath,
		"--dataset", task.DatasetPath,
		"--output-dir", outputDir,
		"--base-model", task.BaseModel,
		"--epochs", strconv.Itoa(task.TargetMetrics.Epochs),
		"--learning-rate", fmt.Sprintf("%.6f", task.TargetMetrics.LearningRate),
		"--batch-size", strconv.Itoa(task.TargetMetrics.BatchSize),
	}

	cmd := exec.CommandContext(ctx, s.pythonBinary(), args...)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = io.MultiWriter(logFile, &stdout)
	cmd.Stderr = io.MultiWriter(logFile, &stderr)

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message == "" {
			message = err.Error()
		}
		return CompletedTask{}, errors.New(message)
	}

	result, err := parseRunnerOutput(stdout.Bytes())
	if err != nil {
		return CompletedTask{}, err
	}

	adapterPath := strings.TrimSpace(result.AdapterPath)
	if adapterPath == "" {
		adapterPath = s.DefaultAdapterPath(task.TaskID)
	}
	if _, err := os.Stat(adapterPath); err != nil {
		return CompletedTask{}, fmt.Errorf("trainer did not produce adapter file: %w", err)
	}

	completed := CompletedTask{
		TaskID:       task.TaskID,
		Type:         task.Type,
		CompletedAt:  time.Now().UTC(),
		ChatIDs:      append([]string(nil), task.ChatIDs...),
		MessageCount: task.MessageCount,
		BaseModel:    task.BaseModel,
		AdapterID:    task.TaskID,
		AdapterPath:  adapterPath,
		Accuracy:     result.Accuracy,
		Loss:         result.Loss,
		DatasetPath:  task.DatasetPath,
		LogPath:      logPath,
	}

	if s.cfg.Training.AutoApplyAdapters {
		if err := s.applyAdapter(adapterPath); err != nil {
			completed.ApplyError = err.Error()
		} else {
			completed.AutoApplied = true
		}
	}

	return completed, nil
}

func (s *Service) loadQueueLocked() (Queue, error) {
	file, err := os.Open(s.cfg.Paths.TrainingQueueFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			queue := Queue{
				AutoTrainingEnabled: s.cfg.Training.AutoTrainingEnabled,
				PendingTasks:        []Task{},
				CompletedTasks:      []CompletedTask{},
			}
			if err := s.saveQueueLocked(queue); err != nil {
				return Queue{}, err
			}
			return queue, nil
		}
		return Queue{}, err
	}
	defer file.Close()

	var queue Queue
	if err := json.NewDecoder(file).Decode(&queue); err != nil {
		return Queue{}, err
	}
	queue.AutoTrainingEnabled = s.cfg.Training.AutoTrainingEnabled
	if queue.PendingTasks == nil {
		queue.PendingTasks = []Task{}
	}
	if queue.CompletedTasks == nil {
		queue.CompletedTasks = []CompletedTask{}
	}
	return queue, nil
}

func (s *Service) saveQueueLocked(queue Queue) error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.Paths.TrainingQueueFile), 0o755); err != nil {
		return err
	}

	queue.AutoTrainingEnabled = s.cfg.Training.AutoTrainingEnabled
	data, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		return err
	}

	tempPath := s.cfg.Paths.TrainingQueueFile + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tempPath, s.cfg.Paths.TrainingQueueFile)
}

func (s *Service) collectDataset(ctx context.Context, chatIDs []string) ([]DatasetRow, error) {
	rows := make([]DatasetRow, 0, 128)

	for _, chatID := range chatIDs {
		value, err := s.chatStore.Get(ctx, chatID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, rowsFromChat(value)...)
	}

	if len(rows) == 0 {
		return nil, errors.New("no user/assistant pairs found in selected chats")
	}

	return rows, nil
}

func rowsFromChat(value chat.Chat) []DatasetRow {
	rows := make([]DatasetRow, 0)

	for index := 0; index < len(value.Messages)-1; index++ {
		userMessage := value.Messages[index]
		reply := value.Messages[index+1]
		if userMessage.Role != "user" || reply.Role != "assistant" {
			continue
		}

		userText := strings.TrimSpace(userMessage.Content)
		replyText := strings.TrimSpace(reply.Content)
		if userText == "" || replyText == "" {
			continue
		}

		rows = append(rows, DatasetRow{
			Instruction:  userText,
			Response:     replyText,
			ChatID:       value.ID,
			UserMessage:  userMessage.ID,
			ReplyMessage: reply.ID,
		})
	}

	return rows
}

func (s *Service) writeDatasetLocked(taskID string, rows []DatasetRow) (string, error) {
	if err := os.MkdirAll(s.cfg.Paths.TrainingDatasetsDir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(s.cfg.Paths.TrainingDatasetsDir, taskID+".jsonl")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			return "", err
		}
	}

	return path, nil
}

func (s *Service) buildManualCommand(task Task) string {
	script := s.cfg.Training.ScriptPath
	if resolved, err := s.resolveExistingPath(script); err == nil {
		script = resolved
	}
	return fmt.Sprintf(
		"%s %s --dataset %s --output-dir %s --base-model %s --epochs %d --learning-rate %.6f --batch-size %d",
		s.pythonBinary(),
		script,
		task.DatasetPath,
		filepath.Join(s.cfg.Paths.TrainingAdaptersDir, task.TaskID),
		strconv.Quote(task.BaseModel),
		task.TargetMetrics.Epochs,
		task.TargetMetrics.LearningRate,
		task.TargetMetrics.BatchSize,
	)
}

func (s *Service) newTaskLocked(kind string, chatIDs []string, baseModel string, epochs int, learningRate float64, batchSize int, rows []DatasetRow) (Task, error) {
	metrics := s.defaultMetrics(epochs, learningRate, batchSize)
	baseModel = strings.TrimSpace(baseModel)
	if baseModel == "" {
		baseModel = s.cfg.Training.BaseModelName
	}

	task := Task{
		TaskID:        ids.New("train"),
		Status:        "prepared",
		Type:          kind,
		CreatedAt:     time.Now().UTC(),
		ChatIDs:       append([]string(nil), chatIDs...),
		MessageCount:  len(rows),
		BaseModel:     baseModel,
		TargetMetrics: metrics,
	}
	task.LogPath = s.defaultLogPath(task.TaskID)

	if s.cfg.Training.PrepareDatasetOnManual || kind == "auto" || s.cfg.Training.AutoRun {
		datasetPath, err := s.writeDatasetLocked(task.TaskID, rows)
		if err != nil {
			return Task{}, err
		}
		task.DatasetPath = datasetPath
		task.ManualCommand = s.buildManualCommand(task)
	}

	if s.cfg.Training.Enabled && s.cfg.Training.AutoRun && task.DatasetPath != "" {
		task.Status = "queued"
	}

	return task, nil
}

func (s *Service) defaultMetrics(epochs int, learningRate float64, batchSize int) TargetMetrics {
	metrics := TargetMetrics{
		Epochs:       epochs,
		LearningRate: learningRate,
		BatchSize:    batchSize,
	}
	if metrics.Epochs <= 0 {
		metrics.Epochs = s.cfg.Training.DefaultEpochs
	}
	if metrics.BatchSize <= 0 {
		metrics.BatchSize = s.cfg.Training.DefaultBatchSize
	}
	if metrics.LearningRate <= 0 {
		value, _ := strconv.ParseFloat(s.cfg.Training.DefaultLearningRate, 64)
		if value <= 0 {
			value = 0.0001
		}
		metrics.LearningRate = value
	}
	return metrics
}

func (s *Service) minPairsToTrain() int {
	if s.cfg.Training.MinPairsToTrain > 0 {
		return s.cfg.Training.MinPairsToTrain
	}
	return 10
}

func (s *Service) hasPendingTaskForChatLocked(queue Queue, chatID string) bool {
	for _, task := range queue.PendingTasks {
		if len(task.ChatIDs) == 1 && task.ChatIDs[0] == chatID && task.Status != "failed" {
			return true
		}
	}
	return false
}

func (s *Service) lastCompletedPairsForChatLocked(queue Queue, chatID string) int {
	maxCount := 0
	for _, task := range queue.CompletedTasks {
		if len(task.ChatIDs) == 1 && task.ChatIDs[0] == chatID && task.MessageCount > maxCount {
			maxCount = task.MessageCount
		}
	}
	return maxCount
}

func (s *Service) notify() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

func (s *Service) pythonBinary() string {
	value := strings.TrimSpace(s.cfg.Training.PythonBinary)
	if value != "" && value != "python3" && value != "python" {
		return value
	}
	if bundled := s.resolveBundledPython(); bundled != "" {
		return bundled
	}
	if value == "" {
		return "python3"
	}
	return value
}

func (s *Service) resolveScriptPath() (string, error) {
	value := strings.TrimSpace(s.cfg.Training.ScriptPath)
	if value == "" {
		value = "scripts/train_lora.py"
	}
	if resolved, err := s.resolveExistingPath(value); err == nil {
		return resolved, nil
	}
	for _, candidate := range []string{"train_lora.py", "scripts/train_lora.py"} {
		if resolved, err := s.resolveExistingPath(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", os.ErrNotExist
}

func (s *Service) resolveExistingPath(value string) (string, error) {
	if value == "" {
		return "", os.ErrNotExist
	}
	if filepath.IsAbs(value) {
		if _, err := os.Stat(value); err != nil {
			return "", err
		}
		return value, nil
	}

	candidates := make([]string, 0, 5)
	candidates = append(candidates, value)
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, value))
	}
	if executable, err := os.Executable(); err == nil {
		execDir := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(execDir, value),
			filepath.Join(execDir, "..", value),
			filepath.Join(execDir, "..", "..", value),
		)
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			absolute, absErr := filepath.Abs(candidate)
			if absErr != nil {
				return candidate, nil
			}
			return absolute, nil
		}
	}

	return "", os.ErrNotExist
}

func (s *Service) defaultLogPath(taskID string) string {
	return filepath.Join(s.cfg.Paths.TrainingLogsDir, taskID+".log")
}

func (s *Service) resolveBundledPython() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	execDir := filepath.Dir(executable)
	candidates := []string{
		filepath.Join(execDir, "python-runtime", "bin", "python3"),
		filepath.Join(execDir, "python-runtime", "bin", "python"),
		filepath.Join(execDir, "python-runtime", "Scripts", "python.exe"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func (s *Service) applyAdapter(adapterPath string) error {
	s.cfg.Llama.ActiveAdapterPath = adapterPath
	if err := s.cfg.Save(); err != nil {
		return err
	}
	if s.llama == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return s.llama.SetActiveAdapterPath(ctx, adapterPath)
}

func parseRunnerOutput(data []byte) (runnerOutput, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return runnerOutput{}, errors.New("trainer returned empty output")
	}

	lines := strings.Split(text, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		var output runnerOutput
		if err := json.Unmarshal([]byte(line), &output); err == nil {
			return output, nil
		}
	}

	return runnerOutput{}, errors.New("trainer output did not contain a valid JSON result")
}
