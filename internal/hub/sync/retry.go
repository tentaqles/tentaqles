package sync

import "time"

// Outbox retry timing (spec §7): a call is tried up to maxTries times per tick while the tracker
// rate-limits; a row that failed for another transient reason waits backoff(attempts) before its
// next try, and fails only after failAfter and minFailAttempts tries.
const (
	firstBackoff    = 15 * time.Second
	maxRowBackoff   = 30 * time.Minute
	failAfter       = 24 * time.Hour
	minFailAttempts = 5
)
