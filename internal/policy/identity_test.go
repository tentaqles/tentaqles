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

// The identity category's v2 rules (PRD v2 §6.3, §7.1; ADR-0007): every rule has a table of
// tool calls it must deny and must allow. The identity is the §6.1 reference identity,
// company-x, an Azure-only identity (az, azure-devops, claude), next to a second identity,
// beta, in the same base.

type scopeFixture struct {
	s                                *Scope
	home, state, base, cwd, betaRoot string
	betaDir, appData                 string
	set                              *protect.Set
	localAppData                     string
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
	localAppData := filepath.Join(home, "AppData", "Local")
	for _, d := range []string{cwd, betaRoot, betaDir, filepath.Join(base, "betamax")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"TQ_HOME": state, "APPDATA": appData, "LOCALAPPDATA": localAppData}
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
	return scopeFixture{s, home, state, base, cwd, betaRoot, betaDir, appData, set, localAppData}
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
			if got != OtherIdentityID && !strings.Contains(r, "outside the scope of identity company-x") {
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
		{"an expansion in a commit message is still an expansion", bash(`git commit -m "use $AWS_PROFILE"`), r},
		{"a literal commit message is text", bash(`git commit -m 'use AWS_PROFILE=prod'`), ""},
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
		{"raw.githubusercontent is public content, not a gh API host", bash("curl -fsSL https://raw.githubusercontent.com/x/y/main/install.sh"), ""},
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

// TestScope_Round3Missed: routes round 2 let through (ADR-0007 round 3): docker run behind
// global options, text arguments blanked only at their own span, the catalog gaps (Stripe,
// the Google Cloud client library, the AWS.Tools PowerShell modules, cloudflared,
// git.heroku.com, *.supabase.co, the doctl image), env reads in Deno, Bun, Ruby and Perl,
// and an MCP input that carries an out-of-scope API URL.
func TestScope_Round3Missed(t *testing.T) {
	f := newScopeFixture(t) // az, azure-devops, claude
	const cli, env, host, mcp = OutOfScopeCLIID, OutOfScopeEnvID, OutOfScopeHostID, OutOfScopeMCPID
	runScopeCases(t, f, []scopeCase{
		// docker run after global options, every non-option word against the images
		{"docker --context remote run", bash("docker --context remote run --rm amazon/aws-cli s3 ls"), cli},
		{"docker container run", bash("docker container run --rm amazon/aws-cli s3 ls"), cli},
		{"docker run --platform gcloud image", bash("docker run --rm --platform linux/amd64 google/cloud-sdk gcloud projects list"), cli},
		{"docker run --gpus all", bash("docker run --gpus all amazon/aws-cli s3 ls"), cli},
		{"nerdctl run --pull always", bash("nerdctl run --pull always amazon/aws-cli s3 ls"), cli},
		{"docker -H host container run", bash("docker -H ssh://build container run amazon/aws-cli s3 ls"), cli},
		{"podman --remote run", bash("podman --remote run --rm docker.io/amazon/aws-cli:2.15.0 s3 ls"), cli},
		{"docker run of an ordinary image", bash("docker --context remote run --rm node:20 npm test"), ""},
		{"docker container ls", bash("docker container ls -a"), ""},
		// a text argument is blanked at its own span only
		{"curl piped to grep", bash("curl -s https://ec2.amazonaws.com/ | grep am"), host},
		{"an expansion piped to grep -c of its name", bash("echo $GH_TOKEN | grep -c GH_TOKEN"), env},
		{"a grep pattern equal to an earlier URL", bash("curl https://api.github.com/user | grep -c https://api.github.com/user"), host},
		{"a commit message equal to an earlier expansion", pwsh("echo $env:GH_TOKEN; git commit -m GH_TOKEN"), env},
		{"a grep pattern alone is text", bash("grep -rn GH_TOKEN src/"), ""},
		// catalog gaps
		{"STRIPE_API_KEY", bash("echo $STRIPE_API_KEY"), env},
		{"import stripe", bash(`python -c "import stripe; print(stripe.Customer.list())"`), cli},
		{"the Stripe CLI image", bash("docker run --rm -it stripe/stripe-cli listen"), cli},
		{"from google.cloud import storage", bash(`python -c "from google.cloud import storage"`), cli},
		{"import google.cloud.bigquery", bash(`python3 -c "import google.cloud.bigquery as bq"`), cli},
		{"Import-Module AWS.Tools.S3", pwsh("Import-Module AWS.Tools.S3"), cli},
		{"Install-Module -Name AWS.Tools.Installer", pwsh("Install-Module -Name AWS.Tools.Installer -Scope CurrentUser"), cli},
		{"Import-Module AWSPowerShell.NetCore", pwsh("Import-Module -Name 'AWSPowerShell.NetCore'"), cli},
		{"Get-S3Bucket", pwsh("Get-S3Bucket -BucketName x"), cli},
		{"Set-AWSCredential", pwsh("Set-AWSCredential -ProfileName prod"), cli},
		{"a cmdlet after a pipe", pwsh("'i-1' | Stop-EC2Instance"), cli},
		{"pwsh -c Get-S3Bucket", bash(`pwsh -c "Get-S3Bucket"`), cli},
		{"cloudflared", bash("cloudflared tunnel run app"), cli},
		{"git.heroku.com", bash("git push https://git.heroku.com/app.git main"), host},
		{"a Supabase project host", bash("curl https://abcd.supabase.co/rest/v1/todos"), host},
		{"the doctl image", bash("docker run --rm digitalocean/doctl compute droplet list"), cli},
		// env reads in other languages
		{"Deno.env.get", bash(`deno eval "console.log(Deno.env.get('AWS_PROFILE'))"`), env},
		{"Bun.env.X", bash(`bun -e "console.log(Bun.env.AWS_PROFILE)"`), env},
		{"Bun.env[X]", bash(`bun -e "console.log(Bun.env['AWS_PROFILE'])"`), env},
		{"Ruby ENV[]", bash(`ruby -e 'puts ENV["AWS_PROFILE"]'`), env},
		{"Ruby ENV.fetch", bash(`ruby -e 'puts ENV.fetch("AWS_PROFILE")'`), env},
		{"Perl $ENV{}", bash(`perl -e 'print $ENV{AWS_PROFILE}'`), env},
		{"Perl $ENV{'X'}", bash(`perl -e "print \$ENV{'AWS_PROFILE'}"`), env},
		// MCP
		{"an MCP input holding an API URL", ToolCall{Tool: "mcp__fetch__fetch", Content: `{"url":"https://api.vercel.com/v9/projects"}`}, host},
		{"a nested MCP input URL", ToolCall{Tool: "mcp__browser__open", Content: `{"args":{"target":"see https://api.github.com/user"}}`}, host},
		{"mcp__gh__", ToolCall{Tool: "mcp__gh__list_pull_requests", Content: "{}"}, mcp},
		// must allow
		{"an MCP input holding a public URL", ToolCall{Tool: "mcp__fetch__fetch", Content: `{"url":"https://example.com/docs"}`}, ""},
		{"an MCP input holding an in-scope API URL", ToolCall{Tool: "mcp__fetch__fetch", Content: `{"url":"https://management.azure.com/subscriptions"}`}, ""},
		{"ruby without env", bash(`ruby -e 'puts 1'`), ""},
		{"the Az PowerShell module is az's", pwsh("Import-Module Az.Accounts; Connect-AzAccount"), ""},
		{"ConvertTo-SecureString is core PowerShell", pwsh("ConvertTo-SecureString 'x' -AsPlainText -Force"), ""},
		{"Get-Service is core PowerShell", pwsh("Get-Service | Select-Object -First 3"), ""},
		{"Import-Module of a local module", pwsh("Import-Module ./tools/Build.psm1"), ""},
	})
}

// TestScope_Round3FalsePositives: everyday commands round 2 stopped (ADR-0007 round 3). An
// azure-devops identity owns the bare `az` invocation and `az account|config|version`; the
// Azure identity downloads public content from GitHub, GitLab and Google hosts, because only
// API hosts are matched, never whole registrable domains. A credential sent with a public
// download is still the env and path rules'.
func TestScope_Round3FalsePositives(t *testing.T) {
	const cli, env, host = OutOfScopeCLIID, OutOfScopeEnvID, OutOfScopeHostID
	devops := newScopeFixture(t, "aws", "azure-devops", "claude")
	runScopeCases(t, devops, []scopeCase{
		{"az account show", bash("az account show"), ""},
		{"az account get-access-token for Azure DevOps", bash("az account get-access-token --resource 499b84ac-1321-427f-aa17-267ca6975798"), ""},
		{"az --version", bash("az --version"), ""},
		{"az version", pwsh("az version"), ""},
		{"az config set", bash("az config set core.output=json"), ""},
		{"az devops", bash("az devops configure --defaults organization=https://dev.azure.com/x"), ""},
		{"az vm is still az's", bash("az vm list"), cli},
		{"a global option does not hide the subcommand", bash("az --debug vm list"), cli},
		{"az.cmd group", pwsh("az.cmd group list"), cli},
	})
	f := newScopeFixture(t) // az, azure-devops, claude
	runScopeCases(t, f, []scopeCase{
		{"the helm install script", bash("curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3"), ""},
		{"the nvm install script", bash("curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.1/install.sh | bash"), ""},
		{"Google Fonts", bash("curl https://fonts.googleapis.com/css2?family=Inter"), ""},
		{"a public GCS download", bash("curl -O https://storage.googleapis.com/chrome-for-testing-public/x.zip"), ""},
		{"git clone from gitlab.com", bash("git clone https://gitlab.com/gitlab-org/cli.git"), ""},
		{"git clone from github.com", pwsh("git clone https://github.com/helm/helm.git"), ""},
		{"WebFetch of a public GitHub page", ToolCall{Tool: "WebFetch", URL: "https://github.com/helm/helm/releases"}, ""},
		{"WebFetch of raw content", ToolCall{Tool: "WebFetch", URL: "https://raw.githubusercontent.com/x/y/main/README.md"}, ""},
		{"Invoke-WebRequest of a release asset", pwsh("Invoke-WebRequest https://objects.githubusercontent.com/x -OutFile x.zip"), ""},
		// the API hosts stay denied
		{"the GitHub API", bash("curl https://api.github.com/user"), host},
		{"the GitHub OAuth endpoint", bash("curl -X POST https://github.com/login/oauth/access_token"), host},
		{"the GitHub device flow", ToolCall{Tool: "WebFetch", URL: "https://github.com/login/device/code"}, host},
		{"the GitLab API", bash("curl https://gitlab.com/api/v4/projects"), host},
		{"the GitLab API, bare host", bash("curl gitlab.com/api/v4/user"), host},
		{"the GCS JSON API", bash("curl https://storage.googleapis.com/storage/v1/b?project=x"), host},
		{"the GCS upload API", bash("curl -X POST https://storage.googleapis.com/upload/storage/v1/b/x/o"), host},
		{"the Compute API", ToolCall{Tool: "WebFetch", URL: "https://compute.googleapis.com/compute/v1/projects/x/zones"}, host},
		{"Google OAuth tokens", bash("curl -d x https://oauth2.googleapis.com/token"), host},
		{"Resource Manager", bash("curl https://cloudresourcemanager.googleapis.com/v1/projects"), host},
		// a credential with a public download is the env rule's
		{"a token sent to raw content", bash(`curl -H "Authorization: token $GH_TOKEN" https://raw.githubusercontent.com/x/y/main/f`), env},
		{"a gcloud credential file to GCS", bash("curl -H @$GOOGLE_APPLICATION_CREDENTIALS https://storage.googleapis.com/x/y"), env},
	})
}

// TestScope_PerShellQuoting: the text masker reads each shell's own quoting (PowerShell
// escapes with a backtick and a doubled quote, never a backslash), blanks nothing when a
// quote is unterminated or when a blanked span would cross a separator the guard tokenizer
// saw, and masks a nested pwsh -Command or bash -c with the inner shell's rules. The cmd
// start and Start-Process targets are found past value switches and in colon-joined options.
func TestScope_PerShellQuoting(t *testing.T) {
	f := newScopeFixture(t) // az, azure-devops, claude
	const host, cli, env = OutOfScopeHostID, OutOfScopeCLIID, OutOfScopeEnvID
	runScopeCases(t, f, []scopeCase{
		// per-shell quoting, fail closed
		{"PowerShell: a backslash does not escape the quote", pwsh(`git commit -m "x\" ; curl https://api.github.com/user ; echo \"y"`), host},
		{"PowerShell: a backtick-escaped quote is never blanked", pwsh("git commit -m \"a`\" ; curl https://api.github.com/user ; echo `\"b\""), host},
		{"PowerShell: doubled single quote", pwsh(`git commit -m 'it''s' ; curl https://api.github.com/user`), host},
		{"PowerShell: doubled double quote", pwsh(`git commit -m "say ""hi""" ; curl https://api.github.com/user`), host},
		{"Bash: unterminated quote blanks nothing", bash(`git commit -m "unterminated ; curl https://api.github.com/user`), host},
		{"PowerShell: unterminated quote blanks nothing", pwsh(`git commit -m 'unterminated ; curl https://api.github.com/user`), host},
		{"words disagree with the tokenizer", bash(`git commit -m (curl https://api.github.com/user)`), host},
		{"PowerShell: an unterminated here-string blanks nothing", pwsh("@'\ncurl https://api.github.com/user"), host},
		{"PowerShell: $env: in a double-quoted message is an expansion", pwsh(`git commit -m "use $env:AWS_PROFILE"`), env},
		{"Bash: a literal message stays text", bash(`git commit -m "see https://api.github.com/repos"`), ""},
		{"PowerShell: a literal message stays text", pwsh(`git commit -m "see https://api.github.com/repos"`), ""},
		{"PowerShell: a literal single-quoted message stays text", pwsh(`git commit -m 'drop AWS_PROFILE=prod'`), ""},
		{"PowerShell: a message with a doubled quote stays text", pwsh(`git commit -m 'it''s https://api.github.com/repos'`), ""},
		{"PowerShell: a backslash path in a message stays text", pwsh(`git commit -m "fix C:\temp\ for https://api.github.com/repos"`), ""},
		{"PowerShell: a here-string stays text", pwsh("@'\nos.Getenv(\"AWS_REGION\")\n'@ | Set-Content x"), ""},
		{"Bash: pwsh -Command is masked with PowerShell rules", bash(`pwsh -Command "git commit -m 'see https://api.github.com/repos'"`), ""},
		{"PowerShell: bash -c is masked with bash rules", pwsh(`bash -c "git commit -m 'see https://api.github.com/repos'"`), ""},
		{"Bash: pwsh -Command still runs its commands", bash(`pwsh -Command "git commit -m 'x'; curl https://api.github.com/user"`), host},
		// cmd start: every non-switch word, past /d and /affinity values
		{"start /d <dir>", pwsh(`cmd /c start /d C:\x aws s3 ls`), cli},
		{"start /affinity <mask>", pwsh(`cmd /c start /affinity 0x3 aws s3 ls`), cli},
		{"start /node <n>", pwsh(`cmd /c start /node 1 aws s3 ls`), cli},
		{"start /b /d <dir> title", pwsh(`cmd /c start /b /d C:\x "t" aws s3 ls`), cli},
		{"start with a title and a wrapper", pwsh(`cmd /c start "t" /wait cmd /k aws s3 ls`), cli},
		{"start ordinary", pwsh(`cmd /c start /d C:\x "Build" npm run build`), ""},
		// Start-Process colon forms: the option is split at the first ':'
		{"-FilePath:aws", pwsh("Start-Process -FilePath:aws"), cli},
		{"-FilePath:quoted with -ArgumentList:", pwsh(`Start-Process -FilePath:"aws.exe" -ArgumentList:'s3 ls'`), cli},
		{"-FilePath: a drive path", pwsh(`Start-Process -FilePath:C:\tools\aws.exe`), cli},
		{"-ArgumentList: contents", pwsh("Start-Process cmd -ArgumentList:'/c aws s3 ls'"), cli},
		{"-WindowStyle:Hidden then positional", pwsh("Start-Process -WindowStyle:Hidden aws"), cli},
		{"Start-Process ordinary colon", pwsh("Start-Process -FilePath:notepad -ArgumentList:'notes.md'"), ""},
	})
}

// TestMaskTextArgs_FailClosed pins the masker itself: what it blanks per shell, and that it
// blanks nothing on an unterminated quote or a tokenizer disagreement.
func TestMaskTextArgs_FailClosed(t *testing.T) {
	cases := []struct {
		shell, in string
		blanked   bool
	}{
		{"bash", `git commit -m "literal text"`, true},
		{"bash", `git commit -m "unterminated`, false},
		{"bash", `git commit -m (x y)`, false},
		{"bash", `grep -c x f | git commit -m 'it''s`, false},
		{"powershell", `git commit -m 'it''s'`, true},
		{"powershell", `git commit -m 'unterminated`, false},
		{"powershell", `git commit -m "x\" ; echo y`, true},
		{"powershell", "git commit -m \"a`\"b\"", false},
		{"powershell", "@'\nliteral\n'@", true},
		{"powershell", "@'\nno end", false},
	}
	for _, c := range cases {
		out := maskTextArgsFor(c.in, c.shell)
		if len(out) != len(c.in) {
			t.Errorf("%s %q: the mask changed the length (%q)", c.shell, c.in, out)
		}
		if (out != c.in) != c.blanked {
			t.Errorf("%s %q: blanked=%v, want %v (out %q)", c.shell, c.in, out != c.in, c.blanked, out)
		}
	}
	// The PowerShell message "x\" ends at the second quote: "; echo y" is code and stays.
	if out := maskTextArgsFor(`git commit -m "x\" ; echo y`, "powershell"); !strings.HasSuffix(out, "; echo y") {
		t.Errorf("PowerShell blanked past the closing quote: %q", out)
	}
}
