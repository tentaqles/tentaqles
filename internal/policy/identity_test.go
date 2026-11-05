package policy

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/protect"
	"github.com/tentaqles/tentaqles/internal/providers"
)

// The identity category's v2 rules (PRD v2 §6.3, §7.1; ADR-0006): every rule has a table of
// tool calls it must deny and must allow. The identity is the §6.1 reference identity,
// company-x, an Azure-only identity (az, azure-devops, claude), next to a second identity,
// beta, in the same base.

type scopeFixture struct {
	s                                *Scope
	home, state, base, cwd, betaRoot string
	betaDir, appData                 string
	set                              *protect.Set
}

func newScopeFixture(t testing.TB, scope ...string) scopeFixture {
	t.Helper()
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	state := filepath.Join(home, ".tentaqles")
	base := filepath.Join(home, "repos")
	cwd := filepath.Join(base, "company-x")
	betaRoot := filepath.Join(base, "beta")
	betaDir := filepath.Join(state, "identities", "beta")
	appData := filepath.Join(home, "AppData", "Roaming")
	for _, d := range []string{cwd, betaRoot, betaDir, filepath.Join(base, "betamax")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"TQ_HOME": state, "APPDATA": appData}
	set := protect.New(protect.Inputs{
		Home: state, UserHome: home, Bases: []string{base}, Roots: []string{cwd},
		Env: func(k string) string { return env[k] },
	})
	cat, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if len(scope) == 0 {
		scope = []string{"az", "azure-devops", "claude"}
	}
	embedded := map[string]providers.Match{}
	for _, p := range cat.All() {
		embedded[p.ID] = p.Match
	}
	s := NewScope(ScopeInputs{
		Identity: "company-x", Scope: scope, Providers: cat.All(), Embedded: embedded, Set: set,
		UserHome: home, Env: func(k string) string { return env[k] },
		OtherRoots: map[string]string{betaRoot: "beta"}, OtherDirs: map[string]string{betaDir: "beta"},
	})
	return scopeFixture{s, home, state, base, cwd, betaRoot, betaDir, appData, set}
}

type scopeCase struct {
	name string
	call ToolCall
	want string // the rule id; "" = allowed
}

func bash(c string) ToolCall { return ToolCall{Tool: "Bash", Command: c} }
func pwsh(c string) ToolCall { return ToolCall{Tool: "PowerShell", Command: c} }

func runScopeCases(t *testing.T, f scopeFixture, cases []scopeCase) {
	t.Helper()
	for _, c := range cases {
		d := f.s.Evaluate(f.cwd, c.call)
		got := ""
		if d.Action == Deny {
			got = d.Matched[0].ID
			if d.Matched[0].Category != CategoryIdentity {
				t.Errorf("%s: category %q", c.name, d.Matched[0].Category)
			}
			r := d.Reason()
			if !strings.HasPrefix(r, "BLOCKED [identity/"+got+"]: ") || !strings.Contains(r, "company-x") {
				t.Errorf("%s: reason %q", c.name, r)
			}
			if got != OtherIdentityID && !strings.Contains(r, "scope: az, azure-devops, claude") {
				t.Errorf("%s: the reason does not name the scope: %q", c.name, r)
			}
			if d.Matched[0].Subject == "" {
				t.Errorf("%s: no subject for the log", c.name)
			}
		} else if d.Action != Allow {
			t.Errorf("%s: action %q: the identity category only denies", c.name, d.Action)
		}
		if got != c.want {
			t.Errorf("%s: %s %q %q: got %q, want %q (%s)", c.name, c.call.Tool, c.call.Command, c.call.Path, got, c.want, d.Reason())
		}
	}
}

// TestScope_OutOfScopeCLI: an out-of-scope provider's CLI by name, by path, behind a
// wrapper, inside a nested shell, as `python -m`, through a package runner or Start-Process.
func TestScope_OutOfScopeCLI(t *testing.T) {
	f := newScopeFixture(t)
	const r = OutOfScopeCLIID
	runScopeCases(t, f, []scopeCase{
		// must deny
		{"aws run-instances", bash("aws ec2 run-instances --image-id ami-12345678 --count 1"), r},
		{"aws by Windows path, unquoted (PowerShell)", pwsh(`C:\Program Files\Amazon\AWSCLIV2\aws.exe ec2 run-instances --image-id ami-1`), r},
		{"aws by Windows path, unquoted (Bash)", bash(`C:\Program Files\Amazon\AWSCLIV2\aws.exe ec2 run-instances`), r},
		{"aws by Windows path, call operator", pwsh(`& 'C:\Program Files\Amazon\AWSCLIV2\aws.exe' ec2 describe-instances`), r},
		{"aws by MSYS path, quoted", bash(`"/c/Program Files/Amazon/AWSCLIV2/aws.exe" s3 ls`), r},
		{"aws by absolute path", bash("/usr/local/bin/aws s3 ls"), r},
		{"aws.cmd", pwsh("aws.cmd s3 ls"), r},
		{"python -m awscli", bash("python -m awscli ec2 run-instances"), r},
		{"python3 -m awscli", bash("python3 -m awscli s3 ls"), r},
		{"py -3 -m awscli", pwsh("py -3 -m awscli s3 ls"), r},
		{"python -u -mawscli", bash("python -u -mawscli s3 ls"), r},
		{"sudo aws", bash("sudo aws s3 ls"), r},
		{"env wrapper", bash("env aws s3 ls"), r},
		{"xargs aws", bash("echo bucket | xargs aws s3 rb"), r},
		{"bash -c", bash(`bash -c "aws s3 ls"`), r},
		{"cmd /c", pwsh("cmd /c aws s3 ls"), r},
		{"pwsh -c", bash(`pwsh -c "aws s3 ls"`), r},
		{"eval", bash(`eval "aws s3 ls"`), r},
		{"after cd", bash("cd infra && aws cloudformation deploy --template-file t.yml"), r},
		{"after a pipe", bash("cat ids.txt | aws ec2 terminate-instances"), r},
		{"command substitution", bash("echo $(aws sts get-caller-identity)"), r},
		{"sam", bash("sam deploy"), r},
		{"npx vercel", bash("npx vercel@latest deploy --prod"), r},
		{"pnpm dlx vercel", bash("pnpm dlx vercel deploy"), r},
		{"uvx awscli", bash("uvx awscli s3 ls"), r},
		{"pipx run awscli", bash("pipx run awscli s3 ls"), r},
		{"Start-Process", pwsh("Start-Process aws -ArgumentList 's3','ls'"), r},
		{"Start-Process -FilePath", pwsh(`Start-Process -FilePath "aws.exe" -ArgumentList s3`), r},
		{"gh (git provider is azure-devops)", bash("gh pr list"), r},
		{"gcloud", bash("gcloud compute instances list"), r},
		{"gsutil", bash("gsutil ls gs://x"), r},
		{"bq", bash("bq query 'select 1'"), r},
		{"doctl", bash("doctl compute droplet list"), r},
		{"glab", bash("glab mr list"), r},
		{"encoded PowerShell", pwsh("powershell -EncodedCommand " + encodePS("aws s3 ls")), r},
		// must allow
		{"az", bash("az vm list -o table"), ""},
		{"az devops", bash("az devops project list"), ""},
		{"azd", bash("azd up"), ""},
		{"python -m azure.cli", bash("python -m azure.cli account show"), ""},
		{"claude", bash("claude -p 'summarise'"), ""},
		{"git status", bash("git status"), ""},
		{"git push", bash("git push origin feature/x"), ""},
		{"go test", bash("go test ./... -count=1"), ""},
		{"npm install", bash("npm install"), ""},
		{"npm run build", bash("npm run build"), ""},
		{"ls", bash("ls -la"), ""},
		{"grep in the repo", bash("grep -rn TODO ."), ""},
		{"docker (generic)", bash("docker build -t app ."), ""},
		{"kubectl (generic)", bash("kubectl get pods"), ""},
		{"terraform (generic)", bash("terraform plan"), ""},
		{"psql (generic)", bash("psql -h localhost -U app"), ""},
		{"aws as an argument", bash("echo aws"), ""},
		{"a folder named aws", bash("cat infra/aws/main.tf"), ""},
		{"cd into a folder named aws", bash("cd infra/aws && ls"), ""},
		{"python -m pytest", bash("python -m pytest -q"), ""},
		{"python -m pip", bash("python -m pip install -r requirements.txt"), ""},
		{"a script named like the module", bash("python awscli_notes.py"), ""},
		{"npx eslint", bash("npx eslint ."), ""},
		{"Get-ChildItem", pwsh("Get-ChildItem -Path . -Recurse"), ""},
	})
}

// TestScope_OutOfScopeEnv: a reference to an out-of-scope provider's variable, set, read or
// passed on, in the shell forms of bash, PowerShell and cmd and in inline code.
func TestScope_OutOfScopeEnv(t *testing.T) {
	f := newScopeFixture(t)
	const r = OutOfScopeEnvID
	runScopeCases(t, f, []scopeCase{
		{"assignment prefix", bash("AWS_PROFILE=prod terraform plan"), r},
		{"export", bash("export AWS_ACCESS_KEY_ID=AKIAEXAMPLE"), r},
		{"$NAME", bash("echo $AWS_SECRET_ACCESS_KEY"), r},
		{"${NAME}", bash("echo ${AWS_SESSION_TOKEN}"), r},
		{"$env:", pwsh("$env:AWS_PROFILE = 'prod'"), r},
		{"%NAME%", pwsh("cmd /c echo %AWS_PROFILE%"), r},
		{"env: drive", pwsh("Get-Item env:AWS_PROFILE"), r},
		{"printenv", bash("printenv GITHUB_TOKEN"), r},
		{"unset", bash("unset GH_TOKEN"), r},
		{"python environ", bash(`python -c "import os; print(os.environ['AWS_SECRET_ACCESS_KEY'])"`), r},
		{"python getenv", bash(`python -c "import os; os.getenv('GOOGLE_APPLICATION_CREDENTIALS')"`), r},
		{"node process.env", bash(`node -e "console.log(process.env.VERCEL_TOKEN)"`), r},
		{"docker -e", bash("docker run -e AWS_ACCESS_KEY_ID=x app"), r},
		{"prefix pattern", bash("TF_TOKEN_app_terraform_io=x terraform login"), r},
		{"a real double-quoted expansion", bash(`echo "profile is $AWS_PROFILE"`), r},
		{"a commit message is text", bash(`git commit -m "use $AWS_PROFILE"`), ""},
		{"a single-quoted grep pattern is text", bash(`grep -rn '$AWS_PROFILE' src/`), ""},
		{"an rg pattern is text", bash(`rg 'process.env.AWS_REGION' .`), ""},
		{"lower-case on Windows", pwsh("$env:aws_profile"), r},
		// must allow
		{"an in-scope variable", bash("echo $AZURE_CONFIG_DIR"), ""},
		{"an in-scope ARM variable", bash("export ARM_CLIENT_ID=00000000-0000-0000-0000-000000000000"), ""},
		{"a bare name in a message", bash(`git commit -m "document AWS_PROFILE"`), ""},
		{"a longer name", bash("echo $AWS_PROFILE_NOTES_X"), ""},
		{"HOME", bash("echo $HOME"), ""},
		{"NODE_ENV", bash("export NODE_ENV=production"), ""},
		{"a data heredoc", bash("cat > notes.md <<'EOF'\nexport AWS_PROFILE=prod\nEOF"), ""},
	})
}

// TestScope_OutOfScopePath: a credential path of an out-of-scope provider, in a file tool, in
// any shell word (expanded, relative to a cd, redirected), in inline code and in an MCP input.
func TestScope_OutOfScopePath(t *testing.T) {
	f := newScopeFixture(t)
	const r = OutOfScopePathID
	creds := filepath.Join(f.home, ".aws", "credentials")
	ghHosts := filepath.Join(f.home, ".config", "gh", "hosts.yml")
	cases := []scopeCase{
		{"Read ~/.aws/credentials", ToolCall{Tool: "Read", Path: creds}, r},
		{"Read with a tilde", ToolCall{Tool: "Read", Path: "~/.aws/credentials"}, r},
		{"Read ~/.aws/config", ToolCall{Tool: "Read", Path: filepath.Join(f.home, ".aws", "config")}, r},
		{"Glob ~/.aws", ToolCall{Tool: "Glob", Path: filepath.Join(f.home, ".aws") + "/*"}, r},
		{"Grep gcloud", ToolCall{Tool: "Grep", Path: filepath.Join(f.home, ".config", "gcloud")}, r},
		{"Write gh hosts", ToolCall{Tool: "Write", Path: ghHosts}, r},
		{"Edit gh hosts", ToolCall{Tool: "Edit", Path: ghHosts}, r},
		{"%APPDATA% path", ToolCall{Tool: "Read", Path: filepath.Join(f.appData, "gcloud", "credentials.db")}, r},
		{"cat", bash("cat ~/.aws/credentials"), r},
		{"cp", bash("cp ~/.aws/credentials /tmp/c"), r},
		{"ls the folder", bash("ls ~/.aws"), r},
		{"$HOME", bash("cat $HOME/.config/gh/hosts.yml"), r},
		{"USERPROFILE (cmd)", pwsh(`type %USERPROFILE%\.aws\credentials`), r},
		{"$env:USERPROFILE", pwsh(`Get-Content $env:USERPROFILE\.aws\config`), r},
		{"cd then a relative read", bash("cd ~/.aws && cat credentials"), r},
		{"cd home then a relative read", bash("cd ~ && cat .aws/credentials"), r},
		{"redirect", bash("echo x > ~/.aws/credentials"), r},
		{"inline code", bash(`python -c "print(open('~/.aws/credentials').read())"`), r},
		{"MCP input", ToolCall{Tool: "mcp__filesystem__read_file", Content: `{"path":` + jsonQuote(creds) + `}`}, r},
		// must allow
		{"an in-scope credential folder", ToolCall{Tool: "Read", Path: filepath.Join(f.home, ".azure", "azureProfile.json")}, ""},
		{"an in-scope folder in the shell", bash("ls ~/.azure"), ""},
		{"a repo file", ToolCall{Tool: "Read", Path: filepath.Join(f.cwd, "README.md")}, ""},
		{"a repo folder named .aws", ToolCall{Tool: "Read", Path: ".aws/notes.md"}, ""},
		{"a repo folder named .aws in the shell", bash("cat .aws/notes.md"), ""},
		{"~/.config", bash("ls ~/.config"), ""},
		{"a longer name", ToolCall{Tool: "Read", Path: filepath.Join(f.home, ".aws-notes", "x")}, ""},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, scopeCase{"MSYS spelling", bash("cat " + msys(creds)), r})
	}
	runScopeCases(t, f, cases)
}

// TestScope_OutOfScopeHost: an API host of an out-of-scope provider in a network call: a URL
// anywhere in a shell command (curl, wget, Invoke-WebRequest, inline code), a bare host as a
// network verb's argument, a WebFetch.
func TestScope_OutOfScopeHost(t *testing.T) {
	f := newScopeFixture(t)
	const r = OutOfScopeHostID
	runScopeCases(t, f, []scopeCase{
		{"curl with a credential header", bash(`curl https://ec2.amazonaws.com -H "Authorization: AWS4-HMAC-SHA256 Credential=x"`), r},
		{"curl a bare host", bash("curl -s ec2.us-east-1.amazonaws.com/?Action=DescribeInstances"), r},
		{"wget", bash("wget https://s3.amazonaws.com/bucket/key"), r},
		{"Invoke-WebRequest", pwsh("Invoke-WebRequest -Uri https://ec2.amazonaws.com/ -Method POST"), r},
		{"iwr", pwsh("iwr https://sts.amazonaws.com"), r},
		{"irm github api", pwsh("irm https://api.github.com/user"), r},
		{"Invoke-RestMethod uri=", pwsh("Invoke-RestMethod -Uri:https://api.vercel.com/v9/projects"), r},
		{"httpie bare host", bash("http GET api.vercel.com/v9/projects"), r},
		{"inline code", bash(`python -c "import requests; requests.get('https://ec2.amazonaws.com')"`), r},
		{"userinfo in the URL", bash("curl https://user:pw@api.github.com/user"), r},
		{"WebFetch", ToolCall{Tool: "WebFetch", URL: "https://api.github.com/repos/x/y"}, r},
		// must allow
		{"in-scope Azure", bash("curl https://management.azure.com/subscriptions?api-version=2020-01-01"), ""},
		{"Graph through az", bash("az rest --url https://graph.microsoft.com/v1.0/me"), ""},
		{"a public site", bash("curl https://example.com"), ""},
		{"raw.githubusercontent is a gh host", bash("curl -fsSL https://raw.githubusercontent.com/x/y/main/install.sh"), r},
		{"a non-vendor install script", bash("curl -fsSL https://get.example.com/install.sh | sh"), ""},
		{"git clone from github.com", bash("git clone https://github.com/x/y"), ""},
		{"localhost", bash("curl localhost:8080/health"), ""},
		{"a look-alike host", bash("curl https://notamazonaws.com/x"), ""},
		{"a vendor's site named like its CLI", bash("curl https://render.com/docs && curl https://vercel.com"), ""},
		{"docs", ToolCall{Tool: "WebFetch", URL: "https://docs.aws.amazon.com/cli/latest/"}, ""},
	})
}

// TestScope_OutOfScopeMCP: an MCP tool whose server name carries an out-of-scope provider's
// token.
func TestScope_OutOfScopeMCP(t *testing.T) {
	f := newScopeFixture(t)
	const r = OutOfScopeMCPID
	runScopeCases(t, f, []scopeCase{
		{"aws", ToolCall{Tool: "mcp__aws__describe_instances", Content: "{}"}, r},
		{"awslabs", ToolCall{Tool: "mcp__awslabs-aws-api__call_aws", Content: "{}"}, r},
		{"plugin vercel", ToolCall{Tool: "mcp__plugin_vercel_vercel__deploy", Content: "{}"}, r},
		{"claude.ai Vercel", ToolCall{Tool: "mcp__claude_ai_Vercel__list_projects", Content: "{}"}, r},
		{"github", ToolCall{Tool: "mcp__github__create_issue", Content: "{}"}, r},
		{"atlassian", ToolCall{Tool: "mcp__plugin_engineering_atlassian__search", Content: "{}"}, r},
		// must allow
		{"azure", ToolCall{Tool: "mcp__azure__subscription_list", Content: "{}"}, ""},
		{"azure devops", ToolCall{Tool: "mcp__azure-devops__wit_list", Content: "{}"}, ""},
		{"a server of no provider", ToolCall{Tool: "mcp__memory__read_graph", Content: "{}"}, ""},
		{"playwright", ToolCall{Tool: "mcp__plugin_playwright_playwright__browser_click", Content: "{}"}, ""},
		{"a token inside a word", ToolCall{Tool: "mcp__lawsuit-tracker__list", Content: "{}"}, ""},
	})
}

// TestScope_OtherIdentity: a path under another identity's workspace root or its identity
// folder under the tq home, in any tool.
func TestScope_OtherIdentity(t *testing.T) {
	f := newScopeFixture(t)
	const r = OtherIdentityID
	betaFile := filepath.Join(f.betaRoot, "src", "main.go")
	cases := []scopeCase{
		{"Read", ToolCall{Tool: "Read", Path: betaFile}, r},
		{"Write", ToolCall{Tool: "Write", Path: betaFile}, r},
		{"Grep the root", ToolCall{Tool: "Grep", Path: f.betaRoot}, r},
		{"Glob", ToolCall{Tool: "Glob", Path: f.betaRoot + "/**/*.go"}, r},
		{"relative Read", ToolCall{Tool: "Read", Path: "../beta/README.md"}, r},
		{"its Claude home", ToolCall{Tool: "Write", Path: filepath.Join(f.betaDir, "claude", "projects", "x", "memory", "MEMORY.md")}, r},
		{"shell cat", bash("cat ../beta/.env.example"), r},
		{"shell cd", bash("cd ../beta && git status"), r},
		{"shell absolute", bash("ls " + f.betaRoot), r},
		{"shell inline code", bash(`python -c "open('` + filepath.ToSlash(betaFile) + `').read()"`), r},
		{"MCP input", ToolCall{Tool: "mcp__filesystem__read_file", Content: `{"path":` + jsonQuote(betaFile) + `}`}, r},
		// must allow
		{"own file", ToolCall{Tool: "Read", Path: filepath.Join(f.cwd, "main.go")}, ""},
		{"a name with the other as a prefix", ToolCall{Tool: "Read", Path: filepath.Join(f.base, "betamax", "x")}, ""},
		{"the base itself", bash("ls .."), ""},
		{"Grep the cwd", ToolCall{Tool: "Grep", Path: ""}, ""},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, scopeCase{"MSYS spelling", bash("ls " + msys(f.betaRoot)), r})
	}
	runScopeCases(t, f, cases)
}

// TestScope_InScopeWins: what an in-scope provider claims is never denied because another
// provider lists it too, and a provider taken into scope is allowed.
func TestScope_InScopeWins(t *testing.T) {
	f := newScopeFixture(t, "aws", "az", "azure-devops", "claude")
	for _, c := range []ToolCall{
		bash("aws ec2 run-instances"), bash("python -m awscli s3 ls"), bash("echo $AWS_PROFILE"),
		{Tool: "Read", Path: filepath.Join(f.home, ".aws", "config")}, bash("curl https://ec2.amazonaws.com"),
		{Tool: "mcp__aws__describe_instances", Content: "{}"},
	} {
		if d := f.s.Evaluate(f.cwd, c); d.Action != Allow {
			t.Errorf("%+v: %s", c, d.Reason())
		}
	}
	// az is az's and azure-devops's: with only azure-devops in scope it stays allowed.
	g := newScopeFixture(t, "azure-devops", "claude")
	if d := g.s.Evaluate(g.cwd, bash("az boards work-item show --id 1")); d.Action != Allow {
		t.Errorf("az under azure-devops: %s", d.Reason())
	}
	if d := g.s.Evaluate(g.cwd, bash("azd up")); d.Action != Deny {
		t.Errorf("azd under azure-devops alone was allowed")
	}
}

func TestScope_NilAllowsAll(t *testing.T) {
	var s *Scope
	if d := s.Evaluate("", bash("aws s3 ls")); d.Action != Allow {
		t.Fatal(d.Reason())
	}
}

// encodePS is PowerShell's -EncodedCommand form: base64 of UTF-16LE.
func encodePS(ps string) string {
	u := utf16.Encode([]rune(ps))
	b := make([]byte, len(u)*2)
	for i, r := range u {
		b[2*i] = byte(r)
		b[2*i+1] = byte(r >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func jsonQuote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// msys spells a Windows path the MSYS way: C:\x -> /c/x.
func msys(p string) string {
	p = filepath.ToSlash(p)
	if len(p) > 2 && p[1] == ':' {
		return "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}
