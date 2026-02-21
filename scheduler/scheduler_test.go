package scheduler

import (
	"os"
	"sync"
	"testing"

	"TradingNewsBot/storage"
)

func TestScheduler_RunJobForTest(t *testing.T) {
	url := os.Getenv("SUPABASE_URL")
	if url == "" {
		t.Skip("SUPABASE_URL not set, skipping scheduler integration test")
	}

	store, err := storage.New(url)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer store.Close()

	var notified []struct {
		chatID int64
		html   string
	}
	var mu sync.Mutex
	notify := func(chatID int64, html string) {
		mu.Lock()
		defer mu.Unlock()
		notified = append(notified, struct {
			chatID int64
			html   string
		}{chatID, html})
	}

	sched := New(store, notify, nil)

	// Run jobs - they may or may not send messages depending on DB state
	// (no users -> no notifications). We're mainly verifying they don't panic.
	sched.RunJobForTest("checkIntradayAlerts")
	sched.RunJobForTest("sendMorningSnapshot")
	sched.RunJobForTest("checkAfterHoursAlerts")
	sched.RunJobForTest("unknown") // should log and no-op

	// If there are users with symbols, notified may be non-empty
	mu.Lock()
	n := len(notified)
	mu.Unlock()
	t.Logf("RunJobForTest: %d notifications sent (depends on DB state)", n)
}

func TestScheduler_New(t *testing.T) {
	url := os.Getenv("SUPABASE_URL")
	if url == "" {
		t.Skip("SUPABASE_URL not set, skipping scheduler test")
	}

	store, err := storage.New(url)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	defer store.Close()

	sched := New(store, nil, nil)
	if sched == nil {
		t.Fatal("New returned nil")
	}
	if sched.store != store {
		t.Error("store not set correctly")
	}
}
