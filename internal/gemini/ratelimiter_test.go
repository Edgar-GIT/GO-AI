package gemini

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRateLimiterCooldownAndReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	rl, err := newRateLimiter(filepath.Join(t.TempDir(), "quota_tracking.json"), Limits{
		DailyQuota:   10,
		RequestLimit: 2,
		Cooldown:     time.Minute,
	}, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("new rate limiter: %v", err)
	}

	if err := rl.CanMakeRequest(); err != nil {
		t.Fatalf("expected first request to be allowed: %v", err)
	}
	if err := rl.LogUsage(ModelGemini3FlashPreview, 5, time.Second, true, nil); err != nil {
		t.Fatalf("log usage: %v", err)
	}

	if err := rl.CanMakeRequest(); err == nil {
		t.Fatal("expected cooldown error")
	}

	now = now.Add(time.Minute + time.Second)
	if err := rl.CanMakeRequest(); err != nil {
		t.Fatalf("expected request after cooldown to be allowed: %v", err)
	}
	if err := rl.LogUsage(ModelGemini31ProPreview, 6, time.Second, true, nil); err != nil {
		t.Fatalf("log usage: %v", err)
	}

	now = now.Add(time.Minute + time.Second)
	if err := rl.CanMakeRequest(); err == nil {
		t.Fatal("expected daily quota error")
	}

	now = time.Date(2026, 3, 29, 0, 1, 0, 0, time.UTC)
	if err := rl.CanMakeRequest(); err != nil {
		t.Fatalf("expected quota reset on next day: %v", err)
	}
}
