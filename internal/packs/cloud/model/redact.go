package model

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/secrets"
)

// Redaction markers (spec D §20). A redacted value is replaced, never
// truncated or hashed, so nothing of it survives.
const (
	// RedactedSensitive replaces a value the tool itself marked sensitive:
	// Terraform after_sensitive/before_sensitive/sensitive_values, a
	// sensitive output or variable, a Pulumi secret.
	RedactedSensitive = "[REDACTED:sensitive]"
	// RedactedKey replaces a value under a key that looks like a secret
	// (password, secret, token, connection_string, access/private/... key).
	RedactedKey = "[REDACTED:secret_key]"
)

// PulumiSecretSig is the signature key of a Pulumi secret value
// (apitype.SecretV1: {"4dabf18193072939515e22adb298388d": "1b47...", ...}).
const PulumiSecretSig = "4dabf18193072939515e22adb298388d"

// PulumiSecretText is how pulumi preview prints a secret without
// --show-secrets. Scrub turns it into RedactedSensitive, so every reader
// shows one marker.
const PulumiSecretText = "[secret]"

// MinKnownLen is the shortest sensitive string that Known.Replace hunts for
// elsewhere in a document. Shorter values would redact unrelated words.
const MinKnownLen = 6

// Known collects the string values that a tool marked sensitive, so that a
// copy of the same value elsewhere in the document (a variable default, a
// planned value, a configuration constant) is redacted too.
type Known struct {
	vals map[string]bool
}

// Add records every string leaf of v.
func (k *Known) Add(v any) {
	switch t := v.(type) {
	case string:
		if t == "" || IsRedacted(t) {
			return
		}
		if k.vals == nil {
			k.vals = map[string]bool{}
		}
		k.vals[t] = true
	case json.Number:
		k.Add(t.String())
	case map[string]any:
		for _, x := range t {
			k.Add(x)
		}
	case []any:
		for _, x := range t {
			k.Add(x)
		}
	}
}

// Len is the number of values collected.
func (k *Known) Len() int { return len(k.vals) }

// Replace returns a copy of v where every string equal to a known value is
// RedactedSensitive, and every known value of at least MinKnownLen bytes
// inside a longer string is replaced by it.
func (k *Known) Replace(v any) any {
	if k == nil || len(k.vals) == 0 {
		return v
	}
	long := make([]string, 0, len(k.vals))
	for s := range k.vals {
		if len(s) >= MinKnownLen {
			long = append(long, s)
		}
	}
	sort.Slice(long, func(i, j int) bool { return len(long[i]) > len(long[j]) })
	return k.replace(v, long)
}

func (k *Known) replace(v any, long []string) any {
	switch t := v.(type) {
	case string:
		if k.vals[t] {
			return RedactedSensitive
		}
		for _, s := range long {
			if strings.Contains(t, s) {
				t = strings.ReplaceAll(t, s, RedactedSensitive)
			}
		}
		return t
	case json.Number:
		if k.vals[t.String()] {
			return RedactedSensitive
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for key, x := range t {
			out[key] = k.replace(x, long)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = k.replace(x, long)
		}
		return out
	}
	return v
}

// Mask applies a Terraform sensitivity mask (after_sensitive,
// before_sensitive, sensitive_values) to v and returns the masked copy. A
// mask of true replaces the whole value, whatever its shape, with
// RedactedSensitive; a map or list mask recurses. Every replaced string is
// added to known (which may be nil).
func Mask(v, mask any, known *Known) any {
	switch m := mask.(type) {
	case bool:
		if m && v != nil {
			if known != nil {
				known.Add(v)
			}
			return RedactedSensitive
		}
		return v
	case map[string]any:
		vm, ok := v.(map[string]any)
		if !ok {
			return v
		}
		out := make(map[string]any, len(vm))
		for key, x := range vm {
			out[key] = Mask(x, m[key], known)
		}
		return out
	case []any:
		vl, ok := v.([]any)
		if !ok {
			return v
		}
		out := make([]any, len(vl))
		for i, x := range vl {
			var sub any
			if i < len(m) {
				sub = m[i]
			}
			out[i] = Mask(x, sub, known)
		}
		return out
	}
	return v
}

// Scrub returns a redacted copy of v, for values no tool marked:
//   - a Pulumi secret object, and the "[secret]" text, become RedactedSensitive;
//   - every scalar under a key for which SecretKey is true becomes
//     RedactedKey (booleans and nulls are kept: they carry no secret, and
//     rules read flags such as manage_master_user_password);
//   - a string that holds a JSON object or array (jsonencode, ECS and Batch
//     container definitions, policy documents), or a multi-line YAML
//     mapping (helm values, yamlencode), is decoded, scrubbed the same way
//     and re-encoded in its format; it is kept as it is (after
//     secrets.Redact) when nothing in it changes;
//   - every other string passes through secrets.Redact.
//
// Map keys are kept as they are.
func Scrub(v any) any { return scrub(v, false) }

func scrub(v any, under bool) any {
	switch t := v.(type) {
	case string:
		if t == PulumiSecretText {
			return RedactedSensitive
		}
		if under {
			if t == "" || IsRedacted(t) {
				return t
			}
			if IsReference(t) {
				return RedactString(t)
			}
			return RedactedKey
		}
		if e, ok := scrubEncoded(t); ok {
			return e
		}
		return RedactString(t)
	case json.Number:
		if under {
			return RedactedKey
		}
		return t
	case int, int64, uint64, float64:
		if under {
			return RedactedKey
		}
		return t
	case map[string]any:
		if _, ok := t[PulumiSecretSig]; ok {
			return RedactedSensitive
		}
		if under && isIntrinsicRef(t) {
			return scrub(t, false) // {"Ref": "DbPassword"} names the secret, it is not one
		}
		valueKey := pairValueKey(t)
		out := make(map[string]any, len(t))
		for key, x := range t {
			out[key] = scrub(x, under || SecretKey(key) || key == valueKey)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = scrub(x, under)
		}
		return out
	}
	return v
}

var (
	armExprRe    = regexp.MustCompile(`^\[[^\[].*\]$`)
	dynamicRefRe = regexp.MustCompile(`^\{\{resolve:[a-z0-9-]+:[^{}]+\}\}$`)
)

// IsReference reports a string that points at a secret instead of holding
// one: an ARM template expression ("[parameters('adminPassword')]",
// "[reference(...)]") or a CloudFormation dynamic reference
// ("{{resolve:secretsmanager:acme:SecretString:password}}"). Under a
// secret-looking key these are kept (after secrets.Redact), so rules can
// tell a wired-in secret from a literal one.
func IsReference(s string) bool {
	return (armExprRe.MatchString(s) && !strings.HasPrefix(s, "[REDACTED:")) || dynamicRefRe.MatchString(s)
}

// isIntrinsicRef: a CloudFormation {"Ref": X}, {"Fn::GetAtt": ...} or
// {"Fn::ImportValue": ...} map.
func isIntrinsicRef(m map[string]any) bool {
	if len(m) != 1 {
		return false
	}
	for k := range m {
		return k == "Ref" || k == "Fn::GetAtt" || k == "Fn::ImportValue"
	}
	return false
}

// namedPairs are the {name, value} shapes that carry a key as data: CFN and
// ARM tags, ECS/Lambda/App Service environment lists, CFN parameters.
var namedPairs = [][2]string{
	{"Key", "Value"}, {"key", "value"}, {"Name", "Value"}, {"name", "value"},
	{"ParameterKey", "ParameterValue"}, {"OptionName", "Value"},
}

// pairValueKey returns the value field of a {name, value} pair whose name
// looks like a secret ({"Name": "DB_PASSWORD", "Value": "..."}), or "".
func pairValueKey(m map[string]any) string {
	for _, p := range namedPairs {
		n, ok := m[p[0]].(string)
		if !ok {
			continue
		}
		if _, has := m[p[1]]; has && SecretKey(n) {
			return p[1]
		}
	}
	return ""
}

// RedactString passes s through secrets.Redact (vendor tokens, private keys,
// connection strings with a password, key=value assignments).
func RedactString(s string) string {
	if s == "" {
		return s
	}
	return secrets.Redact(s)
}

// ScrubMap is Scrub for a map, keeping nil as nil.
func ScrubMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out, _ := Scrub(m).(map[string]any)
	return out
}

// secretWords mark a key as secret wherever they appear in it.
var secretWords = []string{"password", "passwd", "passphrase", "secret", "token", "connectionstring", "credential", "privatekey"}

// secretKeySuffixes mark a key ending in one of them ("primary_access_key").
var secretKeySuffixes = []string{
	"accesskey", "secretkey", "primarykey", "secondarykey", "accountkey", "apikey", "masterkey",
	"sharedkey", "clientkey", "saskey", "signingkey", "storagekey", "authkey", "subscriptionkey",
	"encryptionkey", "instrumentationkey", "functionkey", "adminkey", "querykey",
}

// notSecretSuffixes name references to a secret, or settings about one,
// rather than the secret: secret_name, kms_key_id, password_policy,
// token_lifetime, secret_rotation.
var notSecretSuffixes = []string{
	"id", "ids", "arn", "arns", "name", "names", "type", "types", "enabled", "version", "policy",
	"policies", "length", "uri", "url", "endpoint", "rotation", "required", "count", "ttl",
	"lifetime", "expiration", "expiry", "reference", "ref", "path", "source", "location",
	"rotationrules", "rotationperiod", "days", "algorithm", "format", "kind", "mode", "settings",
	"configuration", "config", "ttlseconds", "duration", "issuer", "audience", "audiences",
}

// SecretKey reports whether a key looks like it holds a secret value
// (spec D §20: password, secret, key, token, connection_string). Case,
// "_", "-" and "." are ignored.
func SecretKey(key string) bool {
	k := normKey(key)
	if k == "" {
		return false
	}
	for _, s := range notSecretSuffixes {
		if strings.HasSuffix(k, s) && k != s {
			// "secretid" names a secret; but "token" alone stays secret
			// and "clientsecret" does not end in a reference suffix.
			return false
		}
	}
	for _, w := range secretWords {
		if strings.Contains(k, w) {
			return true
		}
	}
	for _, s := range secretKeySuffixes {
		if strings.HasSuffix(k, s) {
			return true
		}
	}
	return false
}

func normKey(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if r == '_' || r == '-' || r == '.' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
