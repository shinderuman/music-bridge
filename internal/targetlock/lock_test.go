package targetlock

import "testing"

func TestLockRejectsSameTargetUntilUnlocked(t *testing.T) {
	root := t.TempDir()
	unlock, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(root); err == nil {
		t.Fatal("second lock on the same target succeeded")
	}
	unlock()
	secondUnlock, err := Lock(root)
	if err != nil {
		t.Fatalf("lock after unlock failed: %v", err)
	}
	secondUnlock()
}
