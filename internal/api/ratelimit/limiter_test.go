package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterResetsWindow(t *testing.T) {
	limiter := New(2, time.Minute)
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if ok, _ := limiter.allow("user:1", now); !ok {
		t.Fatal("first request denied")
	}
	if ok, _ := limiter.allow("user:1", now); !ok {
		t.Fatal("second request denied")
	}
	if ok, retry := limiter.allow("user:1", now); ok || retry != time.Minute {
		t.Fatalf("third request ok=%v retry=%v", ok, retry)
	}
	if ok, _ := limiter.allow("user:1", now.Add(time.Minute)); !ok {
		t.Fatal("window did not reset")
	}
}
