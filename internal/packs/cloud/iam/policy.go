package iam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// docKind says what a policy document is for. It only decides how the
// statements that carry a Principal are judged: in a trust policy they are
// trust statements (IAM006), in a resource policy resource statements
// (IAM007). Auto decides per statement: a Principal plus an sts:AssumeRole*
// action is trust, any other Principal is a resource statement. Statements
// without a Principal are identity statements (IAM001, IAM002, IAM003,
// IAM005) in every kind.
type docKind int

const (
	docAuto docKind = iota
	docIdentity
	docTrust
	docResource
)

// issue is one rule hit inside a policy document; stmt is the statement's
// index in the document.
type issue struct {
	Rule, Level, Msg string
	Stmt             int
}

// statement is one decoded IAM statement. Only string elements are kept in
// the lists; an element that is an intrinsic (CloudFormation Fn::Sub, a
// Ref) is unknown and never matches "*".
type statement struct {
	Index                   int
	Effect                  string
	Actions, Resources      []string
	HasNotAction, HasNotRes bool
	HasPrincipal            bool
	Principal               any
	Condition               map[string]any
	HasCondition            bool
}

func (s statement) allow() bool {
	return s.Effect == "" || strings.EqualFold(s.Effect, "Allow")
}

// decodeDoc turns a policy document into statements. doc may be the
// decoded object, or a JSON string (Terraform policy attributes, Pulumi
// inputs, standalone files). ok is false when doc is not a policy document
// (a redacted value, an unresolved expression, other JSON).
func decodeDoc(doc any) (stmts []statement, ok bool) {
	if s, isStr := doc.(string); isStr {
		s = strings.TrimSpace(s)
		if s == "" || model.IsRedacted(s) || !strings.HasPrefix(s, "{") {
			return nil, false
		}
		dec := json.NewDecoder(strings.NewReader(s))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		doc = v
	}
	m := model.Map(doc)
	if m == nil {
		return nil, false
	}
	raw, has := lookup(m, "Statement")
	if !has {
		return nil, false
	}
	for i, item := range model.List(raw) {
		sm := model.Map(item)
		if sm == nil {
			continue
		}
		st := statement{Index: i}
		if e, ok := lookup(sm, "Effect"); ok {
			st.Effect, _ = model.Str(e)
		}
		if a, ok := lookup(sm, "Action"); ok {
			st.Actions = model.Strings(a)
		}
		if _, ok := lookup(sm, "NotAction"); ok {
			st.HasNotAction = true
		}
		if r, ok := lookup(sm, "Resource"); ok {
			st.Resources = model.Strings(r)
		}
		if _, ok := lookup(sm, "NotResource"); ok {
			st.HasNotRes = true
		}
		if p, ok := lookup(sm, "Principal"); ok {
			st.HasPrincipal, st.Principal = true, p
		}
		if _, ok := lookup(sm, "NotPrincipal"); ok {
			// NotPrincipal with Allow grants everyone but the named
			// principals: judge it like a "*" principal.
			st.HasPrincipal, st.Principal = true, "*"
		}
		if c, ok := lookup(sm, "Condition"); ok {
			st.Condition = model.Map(c)
			st.HasCondition = len(st.Condition) > 0 || (c != nil && st.Condition == nil)
		}
		stmts = append(stmts, st)
	}
	return stmts, true
}

// lookup reads a policy key. IAM element names are case-sensitive in AWS,
// but a lint that misses "action" would pass a policy AWS rejects anyway,
// so the match is exact first and then case-insensitive.
func lookup(m map[string]any, key string) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

// lintDoc applies the AWS rules to one document.
func lintDoc(doc any, kind docKind, o Options) ([]issue, bool) {
	stmts, ok := decodeDoc(doc)
	if !ok {
		return nil, false
	}
	var out []issue
	for _, s := range stmts {
		if !s.allow() {
			continue
		}
		if !s.HasPrincipal {
			out = append(out, identityRules(s)...)
			continue
		}
		trust := kind == docTrust
		if kind == docAuto {
			trust = assumes(s)
		}
		if trust {
			out = append(out, trustRules(s, o)...)
		} else {
			out = append(out, resourceRules(s)...)
		}
	}
	return out, true
}

func assumes(s statement) bool {
	for _, a := range s.Actions {
		for _, x := range assumeActions {
			if overlaps(a, x) && a != "*" {
				return true
			}
		}
	}
	return false
}

func identityRules(s statement) []issue {
	var out []issue
	add := func(rule, level, format string, args ...any) {
		out = append(out, issue{Rule: rule, Level: level, Msg: fmt.Sprintf("statement %d: ", s.Index) + fmt.Sprintf(format, args...), Stmt: s.Index})
	}
	if s.HasNotAction || s.HasNotRes {
		which := "NotAction"
		if s.HasNotRes {
			which = "NotResource"
			if s.HasNotAction {
				which = "NotAction and NotResource"
			}
		}
		add("IAM003", packs.LevelError, "Allow with %s grants everything not listed; name the actions and resources instead", which)
	}
	var wild []string
	for _, a := range s.Actions {
		if fullWildcard(a) {
			wild = append(wild, a)
		}
	}
	if len(wild) > 0 {
		add("IAM001", packs.LevelError, "Allow with Action %s; list the actions the role needs", quoteList(wild))
	}
	if hasStar(s.Resources) {
		var writes []string
		for _, a := range s.Actions {
			if !readOnly(a) {
				writes = append(writes, a)
			}
		}
		switch {
		case len(writes) > 0:
			add("IAM002", packs.LevelError, "Allow on Resource \"*\" for %s; scope the resources by ARN", quoteList(writes))
		case len(s.Actions) > 0:
			add("IAM002", packs.LevelWarn, "Allow on Resource \"*\" for read-only actions; scope the resources by ARN where you can")
		}
	}
	if wildResource(s.Resources) && !s.HasCondition {
		var esc []string
		for _, a := range s.Actions {
			if fullWildcard(a) {
				continue // IAM001 already reports it
			}
			for _, e := range escalationActions {
				if overlaps(a, e) {
					esc = append(esc, e)
				}
			}
		}
		if len(esc) > 0 {
			add("IAM005", packs.LevelError, "privilege escalation: %s on every resource without a condition; name the roles or add a condition (iam:PassedToService, aws:ResourceTag)", quoteList(dedupe(esc)))
		}
	}
	return out
}

func trustRules(s statement, o Options) []issue {
	var out []issue
	add := func(format string, args ...any) {
		out = append(out, issue{Rule: "IAM006", Level: packs.LevelError, Msg: fmt.Sprintf("statement %d: ", s.Index) + fmt.Sprintf(format, args...), Stmt: s.Index})
	}
	p := principals(s.Principal)
	if p.star {
		add("trust policy with Principal \"*\": anyone can assume the role")
		return out
	}
	if len(o.Accounts) > 0 && !hasCondKey(s.Condition, "sts:ExternalId") {
		for _, a := range p.aws {
			acct, known := accountOf(a)
			if known && !contains(o.Accounts, acct) {
				add("a principal in an account outside packs.cloud.accounts can assume the role without an sts:ExternalId condition")
				break
			}
		}
	}
	for _, f := range p.federated {
		host := federatedHost(f)
		if host == "" {
			continue // SAML and other non-OIDC providers
		}
		if !hasCondKey(s.Condition, host+":aud") && !hasCondKey(s.Condition, host+":app_id") {
			add("OIDC principal %s without a %s:aud condition", host, host)
		}
		if strings.Contains(strings.ToLower(host), githubOIDCHost) {
			vals, found, redacted := condValues(s.Condition, host+":sub")
			switch {
			case !found:
				add("GitHub OIDC principal without a %s:sub condition: any repository can assume the role", host)
			case redacted:
				out = append(out, issue{Rule: "IAM006", Level: packs.LevelWarn, Stmt: s.Index,
					Msg: fmt.Sprintf("statement %d: GitHub OIDC sub condition is redacted in this input; check it names one repository", s.Index)})
			default:
				for _, v := range vals {
					if repoWildcard(v) {
						add("GitHub OIDC sub condition is wildcarded at the repository level; name the repository (repo:<owner>/<repo>:...)")
						break
					}
				}
			}
		}
	}
	return out
}

func resourceRules(s statement) []issue {
	if principals(s.Principal).star && !s.HasCondition {
		return []issue{{Rule: "IAM007", Level: packs.LevelError, Stmt: s.Index,
			Msg: fmt.Sprintf("statement %d: resource policy with Principal \"*\" and no condition makes the resource public", s.Index)}}
	}
	return nil
}

type principalSet struct {
	star                bool
	aws, federated, svc []string
}

func principals(v any) principalSet {
	var p principalSet
	if s, ok := v.(string); ok {
		p.star = strings.TrimSpace(s) == "*"
		return p
	}
	m := model.Map(v)
	for k, val := range m {
		vals := model.Strings(val)
		switch strings.ToLower(k) {
		case "aws":
			p.aws = append(p.aws, vals...)
			if contains(vals, "*") {
				p.star = true
			}
		case "federated":
			p.federated = append(p.federated, vals...)
		case "service":
			p.svc = append(p.svc, vals...)
		case "canonicaluser":
			if contains(vals, "*") {
				p.star = true
			}
		}
	}
	return p
}

var (
	bareAccountRe = regexp.MustCompile(`^[0-9]{12}$`)
	arnAccountRe  = regexp.MustCompile(`^arn:[^:]*:(?:iam|sts)::([0-9]{12}):`)
)

// accountOf returns the account of an AWS principal: a bare id or an IAM
// or STS ARN. known is false for anything else (an intrinsic, a
// ${AWS::AccountId} reference, which is the own account).
func accountOf(p string) (acct string, known bool) {
	p = strings.TrimSpace(p)
	if bareAccountRe.MatchString(p) {
		return p, true
	}
	if m := arnAccountRe.FindStringSubmatch(p); m != nil {
		return m[1], true
	}
	return "", false
}

// federatedHost returns the OIDC issuer host of a federated principal
// ("arn:aws:iam::111111111111:oidc-provider/token.actions.githubusercontent.com",
// "cognito-identity.amazonaws.com", "accounts.google.com"), or "" for SAML.
func federatedHost(f string) string {
	f = strings.TrimSpace(f)
	if i := strings.Index(f, ":oidc-provider/"); i >= 0 {
		return f[i+len(":oidc-provider/"):]
	}
	if strings.Contains(f, ":saml-provider/") || strings.HasPrefix(f, "arn:") || f == "" {
		return ""
	}
	return f // a web identity provider named by host
}

// condValues collects the values of a condition key under every operator.
func condValues(c map[string]any, key string) (vals []string, found, redacted bool) {
	for _, ops := range c {
		for k, v := range model.Map(ops) {
			if !strings.EqualFold(k, key) {
				continue
			}
			found = true
			for _, x := range model.List(v) {
				if model.IsRedacted(x) {
					redacted = true
					continue
				}
				if s, ok := model.Str(x); ok {
					vals = append(vals, s)
				}
			}
		}
	}
	return vals, found, redacted
}

func hasCondKey(c map[string]any, key string) bool {
	_, found, _ := condValues(c, key)
	return found
}

// repoWildcard reports a GitHub sub value that does not pin one repository:
// "*", "repo:*", "repo:acme/*", "repo:acme/app*". "repo:acme/app:*" pins the
// repository and is fine.
func repoWildcard(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || v == "*" {
		return true
	}
	rest, ok := strings.CutPrefix(v, "repo:")
	if !ok {
		return strings.HasPrefix(v, "*")
	}
	repo, _, _ := strings.Cut(rest, ":")
	return repo == "" || strings.ContainsAny(repo, "*?") || !strings.Contains(repo, "/")
}

func hasStar(rs []string) bool {
	for _, r := range rs {
		if strings.TrimSpace(r) == "*" {
			return true
		}
	}
	return false
}

// wildResource is "*" or an ARN whose resource part is only a wildcard
// ("arn:aws:iam::*:role/*", "arn:aws:iam::111111111111:role/*").
var wildArnRe = regexp.MustCompile(`^arn:[^:]*:[^:]*:[^:]*:[^:]*:(?:[A-Za-z-]+/)?\*$`)

func wildResource(rs []string) bool {
	for _, r := range rs {
		r = strings.TrimSpace(r)
		if r == "*" || wildArnRe.MatchString(r) {
			return true
		}
	}
	return false
}

func quoteList(xs []string) string {
	q := make([]string, len(xs))
	for i, x := range xs {
		q[i] = fmt.Sprintf("%q", x)
	}
	return strings.Join(q, ", ")
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if strings.EqualFold(strings.TrimSpace(y), x) {
			return true
		}
	}
	return false
}

// statementLines returns the 1-based line of each element of the top-level
// Statement array of a JSON document (nil when there is none, or a single
// Statement object, which gets the line of the object).
func statementLines(raw []byte) []int {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil
		}
		key, _ := kt.(string)
		if key != "Statement" {
			var skip json.RawMessage
			if dec.Decode(&skip) != nil {
				return nil
			}
			continue
		}
		start := valueStart(raw, int(dec.InputOffset()))
		if start >= len(raw) {
			return nil
		}
		if raw[start] != '[' {
			return []int{lineAt(raw, start)}
		}
		if _, err := dec.Token(); err != nil { // [
			return nil
		}
		var lines []int
		for dec.More() {
			lines = append(lines, lineAt(raw, valueStart(raw, int(dec.InputOffset()))))
			var skip json.RawMessage
			if dec.Decode(&skip) != nil {
				return lines
			}
		}
		return lines
	}
	return nil
}

// valueStart skips whitespace, ':' and ',' from off.
func valueStart(raw []byte, off int) int {
	for off < len(raw) {
		switch raw[off] {
		case ' ', '\t', '\r', '\n', ':', ',':
			off++
		default:
			return off
		}
	}
	return off
}

func lineAt(raw []byte, off int) int { return bytes.Count(raw[:off], []byte("\n")) + 1 }
