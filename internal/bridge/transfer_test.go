package bridge

import (
	"testing"
	"time"
)

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("あいうえお", 20); got != "あいうえお" {
		t.Fatalf("short title = %q", got)
	}
	if got := truncateRunes("あいうえおかきくけこさしすせそたちつてと", 20); got != "あいうえおかきくけこさしすせそたちつてと" {
		t.Fatalf("20-rune title = %q", got)
	}
	if got := truncateRunes("あいうえおかきくけこさしすせそたちつてとな", 20); got != "あいうえおかきくけこさしすせそたちつてと…" {
		t.Fatalf("long title = %q", got)
	}
}

func TestTransferEstimateUsesCompletedTransferTime(t *testing.T) {
	rate, eta := transferEstimate(300, 100, 10*time.Second)
	if rate != 10 {
		t.Fatalf("rate = %d, want 10", rate)
	}
	if eta != 20*time.Second {
		t.Fatalf("eta = %s, want 20s", eta)
	}
	if rate, eta := transferEstimate(300, 100, 0); rate != 0 || eta != 0 {
		t.Fatalf("zero elapsed estimate = %d, %s; want 0, 0s", rate, eta)
	}
}
