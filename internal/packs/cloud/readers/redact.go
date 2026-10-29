package readers

import (
	"bytes"
	"fmt"

	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// Redact returns the redacted copy of a whole input document, as compact
// JSON with a trailing newline, for the plan store's redacted file
// (spec D §7.1, §20). The copy reads back with the same reader to the same
// resources, addresses and actions.
//
//   - tfplan: every value marked in after_sensitive / before_sensitive
//     (resource_changes, resource_drift, output_changes), every sensitive
//     output and sensitive_values entry (planned_values, prior_state), the
//     value and default of every sensitive variable, and every copy of any
//     of those values elsewhere become [REDACTED:sensitive];
//   - changeset: the Before/After/PreviousDeploymentContext strings are
//     decoded, scrubbed and re-encoded;
//   - every kind: model.Scrub over the whole document (secret-looking keys,
//     Pulumi secrets, internal/secrets patterns).
//
// A YAML CloudFormation template comes back as JSON, with intrinsics in long
// form. An unknown kind or a document that is not of the kind is an error;
// a caller that cannot redact must not store or show the document.
func Redact(k model.Kind, raw []byte) ([]byte, error) {
	if err := checkSize(raw); err != nil {
		return nil, err
	}
	if For(k) == nil {
		return nil, fmt.Errorf("%w: kind %q", ErrUnknownFormat, k)
	}
	// The reader validates the format (and so the shape the redaction
	// relies on) before anything is rewritten.
	if _, err := Read(k, "redact", raw, Options{}); err != nil {
		return nil, err
	}
	var root any
	if k == model.KindCFN && !isJSONObject(raw) {
		v, _, err := parseCFNYAML(trimBOM(raw))
		if err != nil {
			return nil, err
		}
		root = v
	} else {
		d, err := decodeJSON(raw, -1)
		if err != nil {
			return nil, err
		}
		root = d.root
	}
	top := asMap(root)
	var out any
	switch k {
	case model.KindTFPlan:
		out = redactTFPlan(top)
	case model.KindChangeSet:
		v, err := redactChangeSet(top)
		if err != nil {
			return nil, err
		}
		out = v
	default:
		out = model.Scrub(top)
	}
	return encodeJSON(out)
}

func isJSONObject(raw []byte) bool {
	t := bytes.TrimSpace(trimBOM(raw))
	return len(t) > 0 && t[0] == '{'
}
