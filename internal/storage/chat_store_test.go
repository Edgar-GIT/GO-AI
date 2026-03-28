package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gopher-ai/internal/chat"
)

func TestChatStoreCreateListAndDelete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	chatsDir := filepath.Join(root, "chats")
	trashDir := filepath.Join(root, "trash")
	if err := os.MkdirAll(chatsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		t.Fatal(err)
	}

	store := NewChatStore(chatsDir, trashDir)
	value := chat.New("Concurrency", "gemini-3.1-pro-preview")
	value.AddMessage(chat.NewMessage("user", "How do goroutines work?", nil))

	if err := store.Create(context.Background(), value); err != nil {
		t.Fatalf("create chat: %v", err)
	}

	got, err := store.Get(context.Background(), value.ID)
	if err != nil {
		t.Fatalf("get chat: %v", err)
	}
	if got.ID != value.ID {
		t.Fatalf("unexpected chat id: got %s want %s", got.ID, value.ID)
	}

	items, err := store.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("list chats: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 chat, got %d", len(items))
	}

	if err := store.Delete(context.Background(), value.ID, false); err != nil {
		t.Fatalf("soft delete chat: %v", err)
	}

	if _, err := os.Stat(filepath.Join(trashDir, value.ID+".json")); err != nil {
		t.Fatalf("chat was not moved to trash: %v", err)
	}
}
