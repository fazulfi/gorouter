package postgres

import (
	"context"
	"errors"
	"testing"
)

func TestAdvisoryLock_Release_Nil(t *testing.T) {
	var l *AdvisoryLock = nil
	if err := l.Release(context.Background()); err != nil {
		t.Errorf("Release on nil lock should return nil, got: %v", err)
	}
}

func TestAdvisoryLock_Release_AlreadyReleased(t *testing.T) {
	l := &AdvisoryLock{released: true}
	err := l.Release(context.Background())
	if !errors.Is(err, ErrLockReleased) {
		t.Errorf("expected ErrLockReleased, got: %v", err)
	}
}

func TestAdvisoryLock_LockID_Nil(t *testing.T) {
	var l *AdvisoryLock = nil
	if id := l.LockID(); id != 0 {
		t.Errorf("LockID on nil lock should be 0, got %d", id)
	}
}

func TestAdvisoryLock_LockID_Value(t *testing.T) {
	l := &AdvisoryLock{lockID: 42}
	if id := l.LockID(); id != 42 {
		t.Errorf("LockID = %d, want 42", id)
	}
}

func TestAdvisoryLock_LockID_Runtime(t *testing.T) {
	l := &AdvisoryLock{lockID: RuntimeLockID}
	if id := l.LockID(); id != RuntimeLockID {
		t.Errorf("LockID = %d, want %d", id, RuntimeLockID)
	}
}

func TestAcquireAdvisoryLock_NilPool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pool, err := brokenPool()
	if err != nil {
		t.Skipf("cannot create broken pool: %v", err)
	}
	defer pool.Close()

	_, err = AcquireAdvisoryLock(ctx, pool, 42)
	if err == nil {
		t.Error("AcquireAdvisoryLock with canceled context on broken pool expected error, got nil")
	}
}

func TestAcquireRuntimeLock_AcquireError(t *testing.T) {
	pool, err := brokenPool()
	if err != nil {
		t.Skipf("cannot create broken pool: %v", err)
	}
	defer pool.Close()

	_, err = AcquireRuntimeLock(context.Background(), pool)
	if err == nil {
		t.Error("AcquireRuntimeLock with broken pool expected error, got nil")
	}
}

func TestRuntimeLock_NilPoolError(t *testing.T) {
	pool, err := brokenPool()
	if err != nil {
		t.Skipf("cannot create broken pool: %v", err)
	}
	defer pool.Close()

	release, err := RuntimeLock(context.Background(), pool)
	if err == nil {
		t.Error("RuntimeLock with broken pool expected error, got nil")
	}
	if release != nil {
		t.Error("RuntimeLock with broken pool should return nil release")
	}
}
