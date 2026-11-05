package doctor

import (
	"fmt"
	"strings"

	"github.com/tentaqles/tentaqles/internal/trust"
)

// The signed integrity record (PRD v2 §8.3, ADR-0004): integrity is an
// error naming every covered file that changed since a human signed;
// integrity-key-file warns that the HMAC key is the plain-file fallback;
// integrity-record-legacy warns that a workspace is trusted through a
// marker of an older tq, so its hooks are not covered yet.

func init() {
	Register(Check{Name: "integrity", Global: integrityKeyFile})
}

func integrityKeyFile(_ Deps, add AddFunc) {
	if !trust.KeyFromFile() {
		return
	}
	add("warn", "integrity-key-file", "",
		"the integrity key is the plain file "+trust.KeyFile()+" (the OS keychain was unavailable when it was created): any program running as your user can read it and sign a record",
		"once the keychain works: delete the file and run tq allow <name> for every workspace")
}

// integrityMismatch reports, for a workspace that resolve found untrusted,
// a signed record that exists but fails: the signature, the manifest hash
// or any covered file. It returns true when it reported, so the caller
// skips the plain untrusted finding, whose fix (tq allow) would be wrong.
func integrityMismatch(name, hash string, add AddFunc) bool {
	rep, err := trust.Verify(name)
	if err != nil {
		add("error", "integrity", name, "cannot verify the integrity record: "+err.Error(), "tq hooks verify "+name)
		return true
	}
	if !rep.Exists || rep.Trusted(hash) {
		return false
	}
	if rep.Invalid != "" {
		add("error", "integrity", name, rep.Invalid+" ("+trust.SignedRecordPath(name)+")", "tq hooks verify --accept "+name+" (human only, after checking what changed)")
		return true
	}
	msg := "the manifest is not the one signed (" + rep.SignedAt + ")"
	if n := len(rep.Changes); n > 0 {
		// An edited manifest is among the changes, as the file it is.
		parts := make([]string, 0, n)
		for _, c := range rep.Changes {
			parts = append(parts, c.String())
		}
		msg = fmt.Sprintf("%d covered file(s) changed since the record was signed (%s): %s", n, rep.SignedAt, strings.Join(parts, "; "))
	}
	add("error", "integrity", name, msg, "tq hooks verify "+name+" lists them; a human runs tq hooks verify --accept "+name)
	return true
}

// integrityLegacy warns that a trusted workspace has no signed record: a
// marker of an older tq counts, and its hooks, shims and binary are not
// covered until a human signs.
func integrityLegacy(name, hash string, add AddFunc) {
	if trust.HasRecord(name, hash) {
		return
	}
	add("warn", "integrity-record-legacy", name,
		"trusted through a marker of an older tq; the hooks, the settings, the shims and the tq binary are not covered by a signed record yet",
		"tq allow "+name+" (or tq migrate manifests --apply)")
}
