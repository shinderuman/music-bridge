package bridge

import "testing"

func TestSafeName(t *testing.T) {
	if got := safeName(" A/B "); got != "AB" {
		t.Fatalf("safeName = %q", got)
	}
	if got := safeName(" "); got != "Unknown" {
		t.Fatalf("empty safeName = %q", got)
	}
}
