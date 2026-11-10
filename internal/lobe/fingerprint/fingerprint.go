package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
)

// version is the rule version of S-08. Changing a rule of steps 1 to 4 needs a new prefix.
const version = "fp1"

// Of returns the fingerprint and the template of a failed call: the tool class of tool, the signature of
// errText, its template, and the hash of those (S-08 step 4). Equal templates of one class give one fp.
// errText is taken as the stored, scrubbed error of the event; Of does not scrub.
func Of(tool, errText string) (fp, template string) {
	class := Class(tool)
	template = Template(Signature(errText))
	sum := sha256.Sum256([]byte(version + "\x00" + class + "\x00" + template))
	return "fp_" + hex.EncodeToString(sum[:])[:16], template
}
