package trust

import (
	"sync"
	"sync/atomic"
)

// Within a memo scope, each workspace's record is verified once, and the
// key and the legacy list are read once: the hook and the prompt paths
// (tq claude-hook, tq env, tq run, tq doctor) open a scope for the command,
// which in production is the life of the process, so one pre-tool-use call
// verifies once however many times resolve and doctor ask. Every write of a
// record, a tombstone, a key or the legacy list clears it. Outside a scope
// nothing is cached; nothing is ever cached on disk.
var memo struct {
	sync.Mutex
	depth  int
	verify map[string]verifyResult
	key    *keyState
	legacy *legacyState
}

type verifyResult struct {
	rep Report
	err error
}

// verifications counts full verifications (Verify calls that read the
// record and hashed the covered files).
var verifications atomic.Int64

// Verifications is the number of full verifications this process made.
// The tests use it to pin "once per command"; tq does not report it.
func Verifications() int64 { return verifications.Load() }

// MemoScope opens a memo scope and returns the func that closes it; scopes
// nest, and the memo is cleared when the outermost one closes.
func MemoScope() (end func()) {
	memo.Lock()
	memo.depth++
	memo.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			memo.Lock()
			memo.depth--
			if memo.depth == 0 {
				clearLocked()
			}
			memo.Unlock()
		})
	}
}

func clearLocked() {
	memo.verify, memo.key, memo.legacy = nil, nil, nil
}

// invalidate drops everything cached: tq wrote a record, a key or the list.
func invalidate() {
	memo.Lock()
	clearLocked()
	memo.Unlock()
}

func memoVerify(name string) (verifyResult, bool) {
	memo.Lock()
	defer memo.Unlock()
	if memo.depth == 0 || memo.verify == nil {
		return verifyResult{}, false
	}
	r, ok := memo.verify[name]
	return r, ok
}

func storeVerify(name string, r verifyResult) {
	memo.Lock()
	defer memo.Unlock()
	if memo.depth == 0 {
		return
	}
	if memo.verify == nil {
		memo.verify = map[string]verifyResult{}
	}
	memo.verify[name] = r
}

func memoKey() (keyState, bool) {
	memo.Lock()
	defer memo.Unlock()
	if memo.depth == 0 || memo.key == nil {
		return keyState{}, false
	}
	return *memo.key, true
}

func storeKey(s keyState) {
	memo.Lock()
	defer memo.Unlock()
	if memo.depth > 0 {
		memo.key = &s
	}
}

func memoLegacy() (legacyState, bool) {
	memo.Lock()
	defer memo.Unlock()
	if memo.depth == 0 || memo.legacy == nil {
		return legacyState{}, false
	}
	return *memo.legacy, true
}

func storeLegacy(s legacyState) {
	memo.Lock()
	defer memo.Unlock()
	if memo.depth > 0 {
		memo.legacy = &s
	}
}
