package detect_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ching-kuo/opsmask/internal/detect"
)

func loadRules(t *testing.T) []detect.Rule {
	t.Helper()
	rules, err := detect.BuiltinRules()
	if err != nil {
		t.Fatalf("BuiltinRules: %v", err)
	}
	return rules
}

func TestScanCountsByType(t *testing.T) {
	rules := loadRules(t)
	input := []byte("ip 10.0.0.1 and 10.0.0.2 mail user@example.com")
	stats, matches, err := detect.Scan(context.Background(), input, rules)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if stats.Masked != 3 {
		t.Fatalf("Masked = %d, want 3", stats.Masked)
	}
	if len(matches) != 3 {
		t.Fatalf("matches = %d, want 3", len(matches))
	}
	if stats.ByType["ip4"] != 2 {
		t.Fatalf("ip4 = %d, want 2", stats.ByType["ip4"])
	}
	if stats.ByType["email"] != 1 {
		t.Fatalf("email = %d, want 1", stats.ByType["email"])
	}
}

func TestScanRespectsCancellation(t *testing.T) {
	rules := loadRules(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := detect.Scan(ctx, []byte("foo"), rules); err == nil {
		t.Fatal("expected ctx error")
	}
}

func TestScanEmptyInput(t *testing.T) {
	rules := loadRules(t)
	stats, matches, err := detect.Scan(context.Background(), nil, rules)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if stats.Masked+stats.Destroyed != 0 || len(matches) != 0 {
		t.Fatalf("expected zero results, got %+v %v", stats, matches)
	}
}

func TestScanReturnsTimely(t *testing.T) {
	// 1 MiB input with no matches; Scan should return quickly even if the
	// caller cancels mid-FindMatches. We do not assert hard wall-clock
	// bounds because regexp's inner loop is uninterruptible, but we cap
	// at 5s to catch pathological regressions.
	rules := loadRules(t)
	input := []byte(strings.Repeat("noise ", 1<<17))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := detect.Scan(ctx, input, rules); err != nil && err != context.Canceled {
		t.Fatalf("Scan: %v", err)
	}
}
