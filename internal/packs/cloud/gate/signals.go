package gate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
)

// Host is what signal and identity reading may touch: variables and small
// local files. The guard backs it with GuardCtx.ReadLocal; tq pack cloud plan
// with a plain bounded read.
type Host struct {
	Getenv func(string) string
	// ReadLocal reads a file; a relative path is relative to the repo root.
	ReadLocal func(path string, max int64) ([]byte, error)
	// ListLocal lists a folder (GuardCtx.ListLocal: the same roots and
	// refusals at plan time and in the guard, so both list the same names).
	ListLocal func(dir string, max int) ([]packs.LocalEntry, error)
	Home      string
}

// localMax caps the small local files (§9.2: 64 KiB).
const localMax = 64 << 10

// Getenv is the hook's environment (tests replace it).
var Getenv = os.Getenv

func (h Host) env(iv Invocation, names ...string) string {
	for _, n := range names {
		if v, ok := iv.Env[n]; ok && v != "" {
			return v
		}
	}
	if h.Getenv != nil {
		for _, n := range names {
			if v := h.Getenv(n); v != "" {
				return v
			}
		}
	}
	return ""
}

// WorkDir is the folder the tool works in, repo-relative: the line's cd
// plus -chdir / --cwd / -C. "?" when unknown.
func WorkDir(iv Invocation) string {
	dir := iv.Dir
	if dir == "" {
		dir = "."
	}
	var sub string
	switch iv.Tool {
	case "terraform", "tofu":
		sub, _ = iv.Flag("chdir")
	case "pulumi":
		sub, _ = iv.Flag("cwd", "C")
	}
	if sub != "" {
		dir = joinDir(dir, sub)
	}
	return dir
}

// Signals are the environment signals of an invocation, in the four tiers
// of §9.2: flags (and the line's cd), the tq dotenv wrapper, variables,
// local files.
func Signals(iv Invocation, h Host) []envgate.Tier {
	var t1, t2, t3, t4 envgate.Tier
	add := func(t *envgate.Tier, key string, vals ...string) {
		for _, v := range vals {
			if v = strings.TrimSpace(v); v != "" {
				*t = append(*t, envgate.Signal{Key: key, Value: v})
			}
		}
	}
	dir := WorkDir(iv)
	if dir != "?" && (iv.Dir != "" || dir != ".") {
		add(&t1, "dirs", dir)
	}
	switch iv.Tool {
	case "terraform", "tofu":
		for _, vf := range iv.FlagAll("var-file") {
			add(&t1, "var_files", vf)
			if dir != "?" && dir != "." && !strings.HasPrefix(vf, "/") {
				add(&t1, "var_files", path.Join(dir, strings.ReplaceAll(vf, `\`, "/")))
			}
		}
	case "pulumi":
		add(&t1, "stacks", iv.FlagAll("stack", "s")...)
	case "az":
		add(&t1, "resource_groups", iv.FlagAll("resource-group", "g")...)
		add(&t1, "subscriptions", iv.FlagAll("subscription")...)
		if len(iv.Verb) >= 2 && iv.Verb[0] == "group" {
			add(&t1, "resource_groups", iv.FlagAll("name", "n")...)
		}
	case "cdk", "cdktf":
		add(&t1, "stacks", iv.Pos...)
		add(&t1, "profiles", iv.FlagAll("profile")...)
		for _, c := range iv.FlagAll("context", "c") {
			if k, v, ok := strings.Cut(c, "="); ok && (k == "env" || k == "environment") {
				add(&t1, envgate.KeyName, v)
			}
		}
	case "aws", "sam":
		add(&t1, "stacks", iv.FlagAll("stack-name")...)
		add(&t1, "profiles", iv.FlagAll("profile")...)
		if iv.Tool == "sam" {
			add(&t1, envgate.KeyName, iv.FlagAll("config-env")...)
		}
	}
	add(&t2, "dotenv", iv.Dotenv...)
	if iv.Unknown {
		// An earlier statement may have changed the variables (and with
		// TF_WORKSPACE the selected workspace): no tier 3 or 4. The caller
		// floors the result at the default environment.
		return []envgate.Tier{t1, t2}
	}
	if iv.Tool == "terraform" || iv.Tool == "tofu" {
		add(&t3, "workspaces", h.env(iv, "TF_WORKSPACE"))
	}
	add(&t3, "profiles", h.env(iv, "AWS_PROFILE", "AWS_DEFAULT_PROFILE"))
	add(&t3, "subscriptions", h.env(iv, "ARM_SUBSCRIPTION_ID"))
	if (iv.Tool == "terraform" || iv.Tool == "tofu") && dir != "?" && h.ReadLocal != nil {
		if b, err := h.ReadLocal(path.Join(dir, ".terraform", "environment"), localMax); err == nil {
			add(&t4, "workspaces", strings.TrimSpace(string(b)))
		}
	}
	return []envgate.Tier{t1, t2, t3, t4}
}

// Identity is the cloud identity a command runs as (§9.6). Unknown parts
// are "".
type Identity struct {
	Subscription string
	Profile      string
	// Unknown: an unmodelled statement may have changed the variables the
	// identity comes from (Invocation.Unknown) and no flag names it. It never
	// equals a recorded identity, so an approve environment denies.
	Unknown bool
}

// String is the evidence form: "profile=<p>;subscription=<id>", "none", or
// "unknown" with whatever a flag named.
func (id Identity) String() string {
	var parts []string
	if id.Unknown {
		parts = append(parts, "unknown")
	}
	if id.Profile != "" {
		parts = append(parts, "profile="+id.Profile)
	}
	if id.Subscription != "" {
		parts = append(parts, "subscription="+strings.ToLower(id.Subscription))
	}
	if len(parts) == 0 {
		return "none"
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// ResolveIdentity reads the identity of iv. Azure (az, terraform, tofu,
// pulumi): --subscription (az), then ARM_SUBSCRIPTION_ID, then the default
// subscription of azureProfile.json (for az, or when the environment
// declares subscriptions). AWS (aws, cdk, sam, terraform, tofu, pulumi):
// --profile (aws, cdk, sam), then AWS_PROFILE / AWS_DEFAULT_PROFILE.
func ResolveIdentity(iv Invocation, h Host, wantSub bool) Identity {
	var id Identity
	aws := iv.Tool == "aws" || iv.Tool == "cdk" || iv.Tool == "cdktf" || iv.Tool == "sam"
	if iv.Unknown {
		// Only the flags count; the variables and the files they point at
		// (AZURE_CONFIG_DIR) are unknown.
		if iv.Tool == "az" {
			id.Subscription, _ = iv.Flag("subscription")
		}
		if aws {
			id.Profile, _ = iv.Flag("profile")
		}
		if (iv.Tool == "az" && id.Subscription == "") || (iv.Tool != "az" && id.Profile == "") || (!aws && iv.Tool != "az") {
			id.Unknown = true
		}
		return id
	}
	if !aws {
		if iv.Tool == "az" {
			id.Subscription, _ = iv.Flag("subscription")
		}
		if id.Subscription == "" {
			id.Subscription = h.env(iv, "ARM_SUBSCRIPTION_ID")
		}
		if id.Subscription == "" && (iv.Tool == "az" || wantSub) {
			id.Subscription = defaultSubscription(iv, h)
		}
	}
	if iv.Tool == "az" {
		return id // az never uses an AWS profile
	}
	if aws {
		id.Profile, _ = iv.Flag("profile")
	}
	if id.Profile == "" {
		id.Profile = h.env(iv, "AWS_PROFILE", "AWS_DEFAULT_PROFILE")
	}
	return id
}

// defaultSubscription reads the isDefault subscription of azureProfile.json
// under AZURE_CONFIG_DIR (or ~/.azure); "" when it cannot.
func defaultSubscription(iv Invocation, h Host) string {
	dir := h.env(iv, "AZURE_CONFIG_DIR")
	if dir == "" {
		if h.Home == "" {
			return ""
		}
		dir = filepath.Join(h.Home, ".azure")
	}
	if h.ReadLocal == nil || !filepath.IsAbs(dir) {
		return ""
	}
	b, err := h.ReadLocal(filepath.Join(dir, "azureProfile.json"), 1<<20)
	if err != nil {
		return ""
	}
	return ParseAzureProfile(b)
}

// ParseAzureProfile returns the default subscription id of an
// azureProfile.json ("" when there is none), tolerating a UTF-8 BOM.
func ParseAzureProfile(b []byte) string {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	var p struct {
		Subscriptions []struct {
			ID        string `json:"id"`
			IsDefault bool   `json:"isDefault"`
		} `json:"subscriptions"`
	}
	if json.Unmarshal(b, &p) != nil {
		return ""
	}
	for _, s := range p.Subscriptions {
		if s.IsDefault {
			return s.ID
		}
	}
	return ""
}

// Drift compares an identity (and the AWS account a plan record holds) with
// the resolved environment and the manifest (§9.6). It returns the reason
// of a mismatch, or "".
func Drift(g envgate.Gate, env string, id Identity, account string, m *manifest.Manifest) string {
	match := g.Match(env)
	check := func(key, what, val, expected string) string {
		if val == "" {
			return ""
		}
		if want := match[key]; len(want) > 0 {
			for _, w := range want {
				if envgate.Matches(key, w, val) {
					return ""
				}
			}
			return fmt.Sprintf("the %s is %s but environment %s expects %s", what, val, env, strings.Join(want, " or "))
		}
		for _, o := range g.Owners(key, val) {
			if o != env {
				return fmt.Sprintf("the %s %s belongs to environment %s, not %s", what, val, o, env)
			}
		}
		if expected != "" && !strings.EqualFold(expected, val) {
			return fmt.Sprintf("the %s is %s but the manifest expects %s", what, val, expected)
		}
		return ""
	}
	var expSub, expProfile string
	if m != nil {
		expSub = m.Identities["az"].ExpectedSubscription
		expProfile = m.Identities["aws"].Profile
	}
	if r := check("subscriptions", "subscription", id.Subscription, expSub); r != "" {
		return r
	}
	if r := check("profiles", "AWS profile", id.Profile, expProfile); r != "" {
		return r
	}
	return check("accounts", "AWS account", account, "")
}

// HashLocal is the evidence hash of a local file, or plans.None when it is
// absent. Over the read cap is an error (the guard denies).
func HashLocal(h Host, p string) (string, error) {
	b, err := h.ReadLocal(p, 1<<20)
	if err != nil {
		if os.IsNotExist(err) || isNotExist(err) {
			return "none", nil
		}
		return "", err
	}
	return packs.HashBytes(b), nil
}

func isNotExist(err error) bool {
	for e := err; e != nil; {
		if os.IsNotExist(e) {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// HashParts combines named hashes into one evidence hash.
func HashParts(parts []string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%s\n", p)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
