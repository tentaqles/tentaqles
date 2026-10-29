package gate

import "strings"

// argSpec says how a tool's flags take values.
type argSpec struct {
	value map[string]bool // take exactly one value (-chdir=x or -chdir x)
	multi map[string]bool // take every following non-flag word (az --parameters a.json b=c)
	bools map[string]bool // never take a value
	// greedy: an unknown flag followed by a non-flag word takes it as its
	// value (aws, az, sam: their verbs come first). Otherwise an unknown
	// flag is a switch (terraform, pulumi, cdk).
	greedy   bool
	greedyAs string // "multi" or "value" for unknown flags in greedy mode
}

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

var specs = map[string]argSpec{
	"terraform": {value: set("chdir", "out", "var-file", "var", "state", "state-out", "backup", "target", "replace", "lock-timeout", "parallelism", "generate-config-out", "plugin-dir")},
	"pulumi": {value: set("stack", "s", "cwd", "C", "plan", "save-plan", "config-file", "target", "t", "replace", "message", "m", "parallel", "p", "color",
		"exec-kind", "exec-agent", "secrets-provider", "policy-pack", "policy-pack-config", "config", "import-file", "target-replace", "exclude", "attach-debugger", "logflow", "verbose", "v", "tracing", "profiling", "memprofilerate", "otel-traces")},
	"cdk": {value: set("app", "a", "context", "c", "profile", "output", "o", "require-approval", "role-arn", "r", "toolkit-stack-name", "parameters",
		"outputs-file", "O", "concurrency", "change-set-name", "method", "m", "plugin", "p", "proxy", "ca-bundle-path", "build", "notification-arns", "tags", "t", "asset-build-concurrency", "progress")},
	"az": {value: set("subscription", "output", "o", "query"), greedy: true, greedyAs: "multi",
		bools: set("debug", "verbose", "only-show-errors", "yes", "y", "no-wait", "what-if", "w", "j", "no-prompt", "confirm-with-what-if", "c", "proceed-if-no-change", "rollback-on-error", "handle-extended-json-format", "deny-settings-apply-to-child-scopes", "help", "h")},
	"aws": {value: set("profile", "region", "output", "endpoint-url", "query", "color", "cli-read-timeout", "cli-connect-timeout", "ca-bundle", "cli-binary-format",
		"stack-name", "change-set-name", "template-file", "template-body", "template-url", "s3-bucket", "s3-prefix", "kms-key-id", "role-arn", "change-set-type", "client-request-token", "description", "on-failure", "timeout-in-minutes", "stack-policy-body", "stack-policy-url"),
		multi:  set("parameter-overrides", "parameters", "capabilities", "tags", "notification-arns", "resources-to-import"),
		bools:  set("debug", "no-verify-ssl", "no-paginate", "no-sign-request", "no-cli-pager", "no-execute-changeset", "fail-on-empty-changeset", "no-fail-on-empty-changeset", "disable-rollback", "no-disable-rollback", "force-upload", "use-json", "include-property-values", "no-include-property-values", "include-nested-stacks", "no-include-nested-stacks", "import-existing-resources", "no-import-existing-resources", "retain-except-on-create", "help"),
		greedy: true, greedyAs: "value"},
	"sam": {value: set("profile", "region", "config-env", "config-file", "stack-name", "template-file", "template", "t", "s3-bucket", "s3-prefix", "image-repository", "kms-key-id", "role-arn", "on-failure"),
		multi:  set("parameter-overrides", "capabilities", "tags", "notification-arns", "image-repositories", "signing-profiles", "resources-to-import"),
		bools:  set("guided", "g", "no-execute-changeset", "confirm-changeset", "no-confirm-changeset", "fail-on-empty-changeset", "no-fail-on-empty-changeset", "resolve-s3", "debug", "force-upload", "use-json", "disable-rollback", "no-disable-rollback", "no-progressbar", "watch", "no-watch", "code", "infra", "resolve-image-repos", "beta-features", "no-beta-features", "help", "no-fail-on-empty-changes"),
		greedy: true, greedyAs: "value"},
}

func init() {
	specs["tofu"] = specs["terraform"]
	specs["cdktf"] = specs["cdk"]
}

// parseArgs splits the words after the tool into positionals and flags.
func parseArgs(tool string, words []Word) (pos []string, flags map[string][]string) {
	sp := specs[tool]
	flags = map[string][]string{}
	for i := 0; i < len(words); i++ {
		x := words[i].Text
		if x == "--" {
			for _, w := range words[i+1:] {
				pos = append(pos, w.Text)
			}
			break
		}
		if !strings.HasPrefix(x, "-") || x == "-" || words[i].Quoted && !strings.HasPrefix(x, "-") {
			pos = append(pos, x)
			continue
		}
		name := strings.TrimLeft(x, "-")
		val, hasEq := "", false
		if k, v, ok := strings.Cut(name, "="); ok {
			name, val, hasEq = k, v, true
		}
		if len(name) > 1 {
			name = strings.ToLower(name) // -C and -c differ; --Stack is --stack
		}
		next := func() (string, bool) {
			if i+1 < len(words) && (!strings.HasPrefix(words[i+1].Text, "-") || words[i+1].Text == "-") {
				i++
				return words[i].Text, true
			}
			return "", false
		}
		switch {
		case hasEq:
			flags[name] = append(flags[name], val)
		case sp.bools[name]:
			flags[name] = append(flags[name], "")
		case sp.multi[name] || (sp.greedy && sp.greedyAs == "multi" && !sp.value[name]):
			got := false
			for i+1 < len(words) && !strings.HasPrefix(words[i+1].Text, "-") {
				i++
				flags[name] = append(flags[name], words[i].Text)
				got = true
			}
			if !got {
				flags[name] = append(flags[name], "")
			}
		case sp.value[name] || sp.greedy:
			if v, ok := next(); ok {
				flags[name] = append(flags[name], v)
			} else {
				flags[name] = append(flags[name], "")
			}
		default:
			flags[name] = append(flags[name], "")
		}
	}
	return pos, flags
}

// parseTool reads the verb and classifies the invocation (§9.1).
func parseTool(iv *Invocation) {
	pos, flags := parseArgs(iv.Tool, iv.Words[iv.ToolAt+1:])
	iv.Flags = flags
	iv.Op = OpOther
	verb := func(n int) []string {
		if len(pos) < n {
			n = len(pos)
		}
		iv.Verb = pos[:n]
		iv.Pos = pos[n:]
		return iv.Verb
	}
	dry := iv.Has("dry-run")
	switch iv.Tool {
	case "terraform", "tofu":
		v := verb(1)
		if len(v) == 0 {
			return
		}
		switch v[0] {
		case "apply":
			iv.Op = OpApply
			if iv.Has("destroy") {
				iv.Op = OpDestroy
			}
		case "destroy":
			iv.Op = OpDestroy
		case "plan":
			iv.Op = OpDry
		}
	case "pulumi":
		v := verb(1)
		if len(v) == 0 {
			return
		}
		switch v[0] {
		case "up", "update":
			iv.Op = OpApply
		case "destroy":
			iv.Op = OpDestroy
		case "preview", "pre":
			iv.Op = OpDry
		case "stack":
			v = verb(2)
			if len(v) == 2 && (v[1] == "rm" || v[1] == "remove") {
				iv.Op = OpDestroy
			}
		}
	case "cdk", "cdktf":
		v := verb(1)
		if len(v) == 0 {
			return
		}
		switch v[0] {
		case "deploy":
			iv.Op = OpApply
		case "destroy":
			iv.Op = OpDestroy
		case "synth", "synthesize", "diff", "ls", "list":
			iv.Op = OpDry
		}
	case "az":
		v := verb(3)
		switch {
		case len(v) == 3 && v[0] == "deployment" && isScope(v[1]):
			switch v[2] {
			case "create":
				iv.Op = OpApply
				if m, _ := iv.Flag("mode"); strings.EqualFold(m, "complete") {
					iv.AlsoDestroys = true
				}
				if iv.Has("what-if", "w") {
					iv.Op = OpDry
				}
			case "delete":
				iv.Op = OpDestroy
			case "what-if", "validate":
				iv.Op = OpDry
			}
		case len(v) == 3 && v[0] == "stack" && (v[1] == "group" || v[1] == "sub" || v[1] == "mg"):
			switch v[2] {
			case "create", "set":
				iv.Op = OpApply
			case "delete":
				iv.Op = OpDestroy
			}
		case len(v) >= 2 && v[0] == "group" && v[1] == "delete":
			verb(2)
			iv.Op = OpDestroy
		default:
			verb(len(v))
		}
	case "aws":
		v := verb(2)
		if len(v) == 2 && v[0] == "cloudformation" {
			switch v[1] {
			case "deploy":
				iv.Op = OpApply
				if iv.Has("no-execute-changeset") {
					iv.Op = OpDry
				}
			case "create-stack", "update-stack", "execute-change-set":
				iv.Op = OpApply
			case "delete-stack":
				iv.Op = OpDestroy
			case "create-change-set", "describe-change-set", "validate-template", "estimate-template-cost", "detect-stack-drift":
				iv.Op = OpDry
			}
		}
	case "sam":
		v := verb(1)
		if len(v) == 0 {
			return
		}
		switch v[0] {
		case "deploy", "sync":
			iv.Op = OpApply
			if iv.Has("no-execute-changeset") {
				iv.Op = OpDry
			}
		case "delete":
			iv.Op = OpDestroy
		}
	}
	if dry && (iv.Op == OpApply || iv.Op == OpDestroy) {
		iv.Op = OpDry
	}
}

func isScope(s string) bool { return s == "group" || s == "sub" || s == "mg" || s == "tenant" }
