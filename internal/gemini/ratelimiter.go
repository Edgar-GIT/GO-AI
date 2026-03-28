package gemini

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Limits struct {
	DailyQuota   int
	RequestLimit int
	Cooldown     time.Duration
}

type QuotaTracking struct {
	Gemini GeminiQuota `json:"gemini"`
}

type GeminiQuota struct {
	TotalTokensUsedToday int                 `json:"totalTokensUsedToday"`
	TotalTokensLimit     int                 `json:"totalTokensLimit"`
	RequestCount         int                 `json:"requestCount"`
	RequestLimit         int                 `json:"requestLimit"`
	LastResetTime        time.Time           `json:"lastResetTime"`
	ResetTime            time.Time           `json:"resetTime"`
	LastRequestTime      time.Time           `json:"lastRequestTime,omitempty"`
	History              []QuotaHistoryEntry `json:"history,omitempty"`
}

type QuotaHistoryEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	TokensUsed int       `json:"tokensUsed"`
	ModelUsed  string    `json:"modelUsed"`
	Success    bool      `json:"success"`
	LatencyMS  int64     `json:"latency"`
	Error      string    `json:"error,omitempty"`
}

type RateLimiter struct {
	path   string
	limits Limits
	now    func() time.Time

	mu    sync.Mutex
	state QuotaTracking
}

func NewRateLimiter(path string, limits Limits) (*RateLimiter, error) {
	return newRateLimiter(path, limits, time.Now)
}

func newRateLimiter(path string, limits Limits, now func() time.Time) (*RateLimiter, error) {
	rl := &RateLimiter{
		path:   path,
		limits: limits,
		now:    now,
		state:  defaultQuotaState(limits, now().UTC()),
	}

	if err := rl.load(); err != nil {
		return nil, err
	}

	return rl, nil
}

func (rl *RateLimiter) CanMakeRequest() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.resetIfNeededLocked()

	now := rl.now().UTC()
	if !rl.state.Gemini.LastRequestTime.IsZero() && rl.limits.Cooldown > 0 {
		waitUntil := rl.state.Gemini.LastRequestTime.Add(rl.limits.Cooldown)
		if now.Before(waitUntil) {
			return fmt.Errorf("rate limited; retry at %s", waitUntil.Format(time.RFC3339))
		}
	}

	if rl.state.Gemini.TotalTokensUsedToday >= rl.state.Gemini.TotalTokensLimit {
		return fmt.Errorf("daily quota exceeded; resets at %s", rl.state.Gemini.ResetTime.Format(time.RFC3339))
	}

	if rl.state.Gemini.RequestCount >= rl.state.Gemini.RequestLimit {
		return fmt.Errorf("daily request limit exceeded; resets at %s", rl.state.Gemini.ResetTime.Format(time.RFC3339))
	}

	return nil
}

func (rl *RateLimiter) LogUsage(model string, tokens int, latency time.Duration, success bool, err error) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.resetIfNeededLocked()

	now := rl.now().UTC()
	rl.state.Gemini.RequestCount++
	rl.state.Gemini.LastRequestTime = now
	if success {
		rl.state.Gemini.TotalTokensUsedToday += tokens
	}

	entry := QuotaHistoryEntry{
		Timestamp:  now,
		TokensUsed: tokens,
		ModelUsed:  model,
		Success:    success,
		LatencyMS:  latency.Milliseconds(),
	}
	if err != nil {
		entry.Error = err.Error()
	}

	rl.state.Gemini.History = append(rl.state.Gemini.History, entry)
	if len(rl.state.Gemini.History) > 100 {
		rl.state.Gemini.History = rl.state.Gemini.History[len(rl.state.Gemini.History)-100:]
	}

	return rl.saveLocked()
}

func (rl *RateLimiter) Snapshot() QuotaTracking {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.resetIfNeededLocked()
	state := rl.state
	state.Gemini.History = append([]QuotaHistoryEntry(nil), state.Gemini.History...)
	return state
}

func (rl *RateLimiter) Reset() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.state = defaultQuotaState(rl.limits, rl.now().UTC())
	return rl.saveLocked()
}

func (rl *RateLimiter) load() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	file, err := os.Open(rl.path)
	if err != nil {
		if os.IsNotExist(err) {
			return rl.saveLocked()
		}
		return err
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&rl.state); err != nil {
		return err
	}

	rl.state.Gemini.TotalTokensLimit = rl.limits.DailyQuota
	rl.state.Gemini.RequestLimit = rl.limits.RequestLimit
	rl.resetIfNeededLocked()
	return nil
}

func (rl *RateLimiter) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(rl.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(rl.state, "", "  ")
	if err != nil {
		return err
	}

	tempPath := rl.path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tempPath, rl.path)
}

func (rl *RateLimiter) resetIfNeededLocked() {
	now := rl.now().UTC()
	if rl.state.Gemini.ResetTime.IsZero() || !now.Before(rl.state.Gemini.ResetTime) {
		rl.state = defaultQuotaState(rl.limits, now)
	}
}

func defaultQuotaState(limits Limits, now time.Time) QuotaTracking {
	resetAt := nextUTCMidnight(now)
	return QuotaTracking{
		Gemini: GeminiQuota{
			TotalTokensUsedToday: 0,
			TotalTokensLimit:     limits.DailyQuota,
			RequestCount:         0,
			RequestLimit:         limits.RequestLimit,
			LastResetTime:        now,
			ResetTime:            resetAt,
			History:              []QuotaHistoryEntry{},
		},
	}
}

func nextUTCMidnight(now time.Time) time.Time {
	nextDay := now.UTC().AddDate(0, 0, 1)
	return time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), 0, 0, 0, 0, time.UTC)
}
