package training

import (
	"context"
	"os"
	"testing"

	"gopher-ai/internal/chat"
	"gopher-ai/internal/config"
	"gopher-ai/internal/storage"
)

func TestEnqueueManualPreparesDataset(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := config.Default(root)
	cfg.Training.AutoRun = false
	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("ensure directories: %v", err)
	}

	chatStore := storage.NewChatStore(cfg.Paths.ChatsDir, cfg.Paths.TrashDir)
	conversation := chat.New("Training Test", cfg.Models.Primary)
	conversation.AddMessage(chat.NewMessage("user", "Explain channels", nil))
	reply := chat.NewMessage("assistant", "Channels coordinate goroutines.", nil)
	reply.Model = "local-llama"
	conversation.AddMessage(reply)
	if err := chatStore.Create(context.Background(), conversation); err != nil {
		t.Fatalf("seed chat: %v", err)
	}

	service := NewService(cfg, chatStore, nil, nil)
	defer service.Close()
	task, err := service.EnqueueManual(context.Background(), ManualRequest{
		ChatIDs: []string{conversation.ID},
	})
	if err != nil {
		t.Fatalf("enqueue manual: %v", err)
	}

	if task.Status != "prepared" {
		t.Fatalf("unexpected task status: %s", task.Status)
	}
	if task.DatasetPath == "" {
		t.Fatal("expected dataset path")
	}
	if _, err := os.Stat(task.DatasetPath); err != nil {
		t.Fatalf("dataset file missing: %v", err)
	}
	if task.MessageCount != 1 {
		t.Fatalf("unexpected message count: %d", task.MessageCount)
	}
}

func TestMaybeEnqueueAutoCreatesTaskAfterThreshold(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := config.Default(root)
	cfg.Training.AutoRun = false
	cfg.Training.MinPairsToTrain = 2
	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("ensure directories: %v", err)
	}

	chatStore := storage.NewChatStore(cfg.Paths.ChatsDir, cfg.Paths.TrashDir)
	conversation := chat.New("Auto Training", cfg.Models.Primary)
	conversation.AddMessage(chat.NewMessage("user", "Explain channels", nil))
	replyA := chat.NewMessage("assistant", "Channels coordinate goroutines.", nil)
	replyA.Model = "local-llama"
	conversation.AddMessage(replyA)
	conversation.AddMessage(chat.NewMessage("user", "Now explain mutexes", nil))
	replyB := chat.NewMessage("assistant", "Mutexes protect shared state.", nil)
	replyB.Model = "local-llama"
	conversation.AddMessage(replyB)
	if err := chatStore.Create(context.Background(), conversation); err != nil {
		t.Fatalf("seed chat: %v", err)
	}

	service := NewService(cfg, chatStore, nil, nil)
	defer service.Close()

	task, queued, err := service.MaybeEnqueueAuto(conversation)
	if err != nil {
		t.Fatalf("maybe enqueue auto: %v", err)
	}
	if !queued {
		t.Fatal("expected auto training task to be queued")
	}
	if task.Type != "auto" {
		t.Fatalf("unexpected task type: %s", task.Type)
	}
	if task.MessageCount != 2 {
		t.Fatalf("unexpected message count: %d", task.MessageCount)
	}
	if task.DatasetPath == "" {
		t.Fatal("expected dataset path")
	}
}
