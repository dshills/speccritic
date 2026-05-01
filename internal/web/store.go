package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/dshills/speccritic/internal/app"
)

type Store struct {
	mu     sync.RWMutex
	max    int
	ttl    time.Duration
	now    func() time.Time
	newID  func() (string, error)
	checks map[string]*StoredCheck
	order  []string
}

type StoredCheck struct {
	ID        string
	CreatedAt time.Time
	ExpiresAt time.Time
	Result    *app.CheckResult
}

func NewStore(max int, ttl time.Duration) (*Store, error) {
	if max <= 0 {
		return nil, fmt.Errorf("max must be > 0")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("ttl must be > 0")
	}
	return &Store{
		max:    max,
		ttl:    ttl,
		now:    time.Now,
		newID:  randomID,
		checks: make(map[string]*StoredCheck),
	}, nil
}

func (s *Store) Save(result *app.CheckResult) (*StoredCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sweepExpiredLocked()

	id, err := s.uniqueIDLocked()
	if err != nil {
		return nil, err
	}
	now := s.now()
	check := &StoredCheck{
		ID:        id,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
		Result:    result,
	}
	s.checks[id] = check
	s.order = append(s.order, id)
	s.evictLocked()
	return check, nil
}

func (s *Store) Get(id string) (*StoredCheck, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	check, ok := s.checks[id]
	if !ok || !s.now().Before(check.ExpiresAt) {
		return nil, false
	}
	return check, ok
}

func (s *Store) SweepExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepExpiredLocked()
}

func (s *Store) StartJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.SweepExpired()
			}
		}
	}()
}

func (s *Store) uniqueIDLocked() (string, error) {
	for i := 0; i < 8; i++ {
		id, err := s.newID()
		if err != nil {
			return "", err
		}
		if _, exists := s.checks[id]; !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("generating unique check id: too many collisions")
}

func (s *Store) evictLocked() {
	for len(s.order) > s.max {
		id := s.order[0]
		s.order = s.order[1:]
		delete(s.checks, id)
	}
}

func (s *Store) sweepExpiredLocked() {
	now := s.now()
	kept := s.order[:0]
	for _, id := range s.order {
		check, ok := s.checks[id]
		if !ok {
			continue
		}
		if !now.Before(check.ExpiresAt) {
			delete(s.checks, id)
			continue
		}
		kept = append(kept, id)
	}
	s.order = kept
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
