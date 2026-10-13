package bot

import "sync"

// keyLocks is a set of mutexes addressed by an int64 key (a chat or game id).
// Entries are never removed: the key space is a handful of chats and games.
type keyLocks struct{ m sync.Map }

func (k *keyLocks) mutex(key int64) *sync.Mutex {
	v, _ := k.m.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// TryLock takes key's lock if it is free. ok=false means someone else holds it;
// otherwise call unlock when done.
func (k *keyLocks) TryLock(key int64) (unlock func(), ok bool) {
	mu := k.mutex(key)
	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

// Lock waits for key's lock.
func (k *keyLocks) Lock(key int64) (unlock func()) {
	mu := k.mutex(key)
	mu.Lock()
	return mu.Unlock
}

// creatingRooms serialises game creation per chat: the "no pending game" check
// and the usdoku room creation (up to 25 s) must not interleave, or a double tap
// makes two games with two watchers.
var creatingRooms keyLocks

// busyCreatingText is the reply to a second create request while one is running.
const busyCreatingText = "⏳ Уже создаю комнату"

// recordingGames serialises auto-record per game (watcher vs «Записать сейчас»).
var recordingGames keyLocks
