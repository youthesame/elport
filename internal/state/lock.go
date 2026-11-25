package state

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	memLocksMu    sync.Mutex
	memLocks      = map[string]*sync.Mutex{}
	onLockContend func()
)

func OnLockContendForTest(fn func()) func() {
	memLocksMu.Lock()
	prev := onLockContend
	onLockContend = fn
	memLocksMu.Unlock()
	return func() {
		memLocksMu.Lock()
		onLockContend = prev
		memLocksMu.Unlock()
	}
}

func memLockFor(key string) *sync.Mutex {
	memLocksMu.Lock()
	defer memLocksMu.Unlock()
	mu, ok := memLocks[key]
	if !ok {
		mu = &sync.Mutex{}
		memLocks[key] = mu
	}
	return mu
}

func Lock(base, entity, eid string) (func(), error) {
	dir, err := Dir(true)
	if err != nil {
		return nil, err
	}
	key := Key(base, entity, eid)
	lockPath := filepath.Join(dir, key+".lock")
	mu := memLockFor(lockPath)
	if !mu.TryLock() {
		memLocksMu.Lock()
		cb := onLockContend
		memLocksMu.Unlock()
		if cb != nil {
			cb()
		}
		mu.Lock()
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	if err := lockFileExclusive(f); err != nil {
		_ = f.Close()
		mu.Unlock()
		return nil, err
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		_ = unlockFile(f)
		_ = f.Close()
		mu.Unlock()
	}, nil
}
