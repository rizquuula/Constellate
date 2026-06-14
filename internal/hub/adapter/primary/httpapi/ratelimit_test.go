package httpapi

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowsUpToMax(t *testing.T) {
	l := newRateLimiter(3, time.Minute)
	now := time.Unix(1000, 0)

	for i := 0; i < 3; i++ {
		ok, ra := l.allow("key", now)
		if !ok {
			t.Fatalf("call %d: expected ok=true", i+1)
		}
		if ra != 0 {
			t.Fatalf("call %d: expected retryAfter=0, got %v", i+1, ra)
		}
	}
}

func TestRateLimiter_DeniesAfterMax(t *testing.T) {
	l := newRateLimiter(3, time.Minute)
	now := time.Unix(1000, 0)

	for i := 0; i < 3; i++ {
		l.allow("key", now) //nolint:errcheck
	}

	ok, ra := l.allow("key", now)
	if ok {
		t.Fatal("expected ok=false after exceeding max")
	}
	if ra <= 0 {
		t.Fatalf("expected positive retryAfter, got %v", ra)
	}
	if ra > time.Minute {
		t.Fatalf("retryAfter %v exceeds window %v", ra, time.Minute)
	}
}

func TestRateLimiter_RecoveryAfterWindow(t *testing.T) {
	l := newRateLimiter(3, time.Minute)
	now := time.Unix(1000, 0)

	for i := 0; i < 3; i++ {
		l.allow("key", now) //nolint:errcheck
	}
	ok, _ := l.allow("key", now)
	if ok {
		t.Fatal("should be denied before window elapses")
	}

	// Advance past the window.
	future := now.Add(time.Minute + time.Second)
	ok2, ra2 := l.allow("key", future)
	if !ok2 {
		t.Fatal("expected ok=true after window elapsed")
	}
	if ra2 != 0 {
		t.Fatalf("expected retryAfter=0 after reset, got %v", ra2)
	}
}

func TestRateLimiter_IndependentKeys(t *testing.T) {
	l := newRateLimiter(2, time.Minute)
	now := time.Unix(1000, 0)

	// Exhaust key "a".
	l.allow("a", now)
	l.allow("a", now)
	okA, _ := l.allow("a", now)
	if okA {
		t.Fatal("key 'a' should be rate-limited")
	}

	// key "b" should still be allowed.
	okB, raB := l.allow("b", now)
	if !okB {
		t.Fatal("key 'b' should not be rate-limited")
	}
	if raB != 0 {
		t.Fatalf("key 'b' retryAfter should be 0, got %v", raB)
	}
}
