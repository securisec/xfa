package store

import "time"

// Liveness thresholds — the single home for every liveness magic number.
// There is deliberately no alive/dead flag anywhere: last_seen_at age is the
// only signal, and it under-counts (read-only and handle-less commands don't
// heartbeat), so display language must stay probabilistic ("last seen 46m
// ago"), never binary ("gone").
const (
	// HeartbeatThrottle caps last_seen_at write frequency per handle, so
	// read-path heartbeats can't destabilize the validated write-contention
	// story (busy-retry + _txlock=immediate).
	HeartbeatThrottle = 60 * time.Second
	// StaleDisplayAge: annotate a handle with "last seen X ago" at/after this.
	StaleDisplayAge = 10 * time.Minute
	// StaleReplyAge: add "answer for the record; don't wait on them" phrasing.
	StaleReplyAge = 30 * time.Minute
)
