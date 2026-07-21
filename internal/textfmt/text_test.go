package textfmt

import "testing"

func TestHumanBytes(t *testing.T) {
	for _, test := range []struct {
		value int64
		want  string
	}{{3, "3.0 B"}, {1024, "1.0 KiB"}, {2 * 1024 * 1024, "2.0 MiB"}} {
		if got := HumanBytes(test.value); got != test.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := TruncateRunes("あいう", 3); got != "あいう" {
		t.Fatalf("short value = %q", got)
	}
	if got := TruncateRunes("あいうえ", 3); got != "あいう…" {
		t.Fatalf("long value = %q", got)
	}
}
