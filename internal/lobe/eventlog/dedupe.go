package eventlog

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// ID is the id of an event whose host gave a call id: "evt_" plus the first 8 hex digits of the
// SHA-256 of host, a zero byte, session, a zero byte, and the call id.
func ID(host, session, callID string) string {
	h := sha256.New()
	h.Write([]byte(host))
	h.Write([]byte{0})
	h.Write([]byte(session))
	h.Write([]byte{0})
	h.Write([]byte(callID))
	return idFrom(h.Sum(nil))
}

// IDWithoutCall is the id of an event with no call id: the hash covers host, session, tool, kind,
// input and the clock in nanoseconds, each followed by a zero byte, so two such events never share an id.
func IDWithoutCall(host, session, tool, kind, input string, nanos int64) string {
	h := sha256.New()
	for _, s := range []string{host, session, tool, kind, input, strconv.FormatInt(nanos, 10)} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return idFrom(h.Sum(nil))
}

// idFrom is "evt_" plus the first 8 hex digits of sum.
func idFrom(sum []byte) string {
	return "evt_" + hex.EncodeToString(sum)[:8]
}
