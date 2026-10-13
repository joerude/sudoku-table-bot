package bot

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestKeyLocksTryLockIsExclusivePerKey(t *testing.T) {
	var k keyLocks
	unlock, ok := k.TryLock(1)
	if !ok {
		t.Fatal("free key must lock")
	}
	if _, ok := k.TryLock(1); ok {
		t.Fatal("held key must refuse a second TryLock")
	}
	if u2, ok := k.TryLock(2); !ok {
		t.Fatal("another key must be independent")
	} else {
		u2()
	}
	unlock()
	if u3, ok := k.TryLock(1); !ok {
		t.Fatal("released key must lock again")
	} else {
		u3()
	}
}

// Many simultaneous creators while one holds the lock: every other TryLock fails.
func TestKeyLocksConcurrentTryLockFailsWhileHeld(t *testing.T) {
	var k keyLocks
	unlock, ok := k.TryLock(42)
	if !ok {
		t.Fatal("first TryLock must win")
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if u, ok := k.TryLock(42); ok {
				wins.Add(1)
				u()
			}
		}()
	}
	wg.Wait()
	unlock()
	if n := wins.Load(); n != 0 {
		t.Fatalf("%d concurrent TryLocks succeeded while the lock was held", n)
	}
}

func TestKeyLocksLockWaits(t *testing.T) {
	var k keyLocks
	unlock := k.Lock(7)
	done := make(chan struct{})
	go func() {
		u := k.Lock(7)
		u()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("second Lock must wait for the first")
	default:
	}
	unlock()
	<-done
}
