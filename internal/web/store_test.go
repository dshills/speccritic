package web

import (
	"context"
	"testing"
	"time"

	"github.com/dshills/speccritic/internal/app"
)

func TestStoreSaveGetAndEvict(t *testing.T) {
	store, err := NewStore(1, time.Hour)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	first, err := store.Save(&app.CheckResult{})
	if err != nil {
		t.Fatalf("Save first: %v", err)
	}
	second, err := store.Save(&app.CheckResult{})
	if err != nil {
		t.Fatalf("Save second: %v", err)
	}
	if _, ok := store.Get(first.ID); ok {
		t.Fatal("first check should have been evicted")
	}
	if _, ok := store.Get(second.ID); !ok {
		t.Fatal("second check missing")
	}
}

func TestStoreExpires(t *testing.T) {
	store, err := NewStore(10, time.Minute)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Now()
	store.now = func() time.Time { return now }

	check, err := store.Save(&app.CheckResult{})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	now = now.Add(2 * time.Minute)
	store.SweepExpired()
	if _, ok := store.Get(check.ID); ok {
		t.Fatal("expired check still present")
	}
}

func TestStoreRetriesIDCollision(t *testing.T) {
	store, err := NewStore(10, time.Hour)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	calls := 0
	store.newID = func() (string, error) {
		calls++
		if calls == 1 {
			return "same", nil
		}
		if calls == 2 {
			return "same", nil
		}
		return "different", nil
	}
	if _, err := store.Save(&app.CheckResult{}); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	second, err := store.Save(&app.CheckResult{})
	if err != nil {
		t.Fatalf("Save second: %v", err)
	}
	if second.ID != "different" {
		t.Fatalf("second ID = %q, want different", second.ID)
	}
}

func TestStoreJanitorStops(t *testing.T) {
	store, err := NewStore(10, time.Millisecond)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	store.StartJanitor(ctx, time.Millisecond)
	cancel()
}
