package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"gopher-ai/internal/chat"
)

var ErrChatNotFound = errors.New("chat not found")

type ListOptions struct {
	Limit  int
	Offset int
	Search string
}

type ChatStore struct {
	dir      string
	trashDir string
	mu       sync.RWMutex
}

func NewChatStore(dir, trashDir string) *ChatStore {
	return &ChatStore{
		dir:      dir,
		trashDir: trashDir,
	}
}

func (s *ChatStore) Create(ctx context.Context, value chat.Chat) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveChat(value)
}

func (s *ChatStore) Get(ctx context.Context, id string) (chat.Chat, error) {
	if err := ctx.Err(); err != nil {
		return chat.Chat{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readChat(s.chatPath(id))
}

func (s *ChatStore) Update(ctx context.Context, value chat.Chat) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveChat(value)
}

func (s *ChatStore) List(ctx context.Context, opts ListOptions) ([]chat.ChatSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []chat.ChatSummary{}, nil
		}
		return nil, err
	}

	search := strings.ToLower(strings.TrimSpace(opts.Search))
	items := make([]chat.ChatSummary, 0, len(entries))

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		item, err := s.readChat(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, err
		}

		if search != "" && !matchesSearch(item, search) {
			continue
		}

		items = append(items, item.Summary())
	}

	slices.SortFunc(items, func(a, b chat.ChatSummary) int {
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})

	start := clamp(opts.Offset, 0, len(items))
	end := len(items)
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}

	return items[start:end], nil
}

func (s *ChatStore) Delete(ctx context.Context, id string, hardDelete bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sourcePath := s.chatPath(id)
	if _, err := os.Stat(sourcePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrChatNotFound
		}
		return err
	}

	if hardDelete {
		return os.Remove(sourcePath)
	}

	if err := os.MkdirAll(s.trashDir, 0o755); err != nil {
		return err
	}

	return os.Rename(sourcePath, filepath.Join(s.trashDir, id+".json"))
}

func (s *ChatStore) chatPath(id string) string {
	return filepath.Join(s.dir, id+".json")
}

func (s *ChatStore) readChat(path string) (chat.Chat, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return chat.Chat{}, ErrChatNotFound
		}
		return chat.Chat{}, err
	}
	defer file.Close()

	var value chat.Chat
	if err := json.NewDecoder(file).Decode(&value); err != nil {
		return chat.Chat{}, err
	}

	value.RefreshMetadata()
	return value, nil
}

func (s *ChatStore) saveChat(value chat.Chat) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}

	path := s.chatPath(value.ID)
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tempPath, path)
}

func matchesSearch(value chat.Chat, search string) bool {
	if strings.Contains(strings.ToLower(value.Title), search) {
		return true
	}

	for _, message := range value.Messages {
		if strings.Contains(strings.ToLower(message.Content), search) {
			return true
		}
	}

	return false
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
