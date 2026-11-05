package policy

import "testing"

// TestScope_ParserDifferentials: the text masker blanks the token the tokenizer saw (by
// offset), never an identical earlier substring, and never double-quoted or unquoted text
// that holds an expansion ($ or a backtick); the docker image and the start/Start-Process
// target are found whatever value options come before them (security review of round 2).
func TestScope_ParserDifferentials(t *testing.T) {
	f := newScopeFixture(t) // az, azure-devops, claude
	cases := []struct {
		name string
		call ToolCall
		deny bool
	}{
		// 1. masking by offset; expansions and substitutions are always scanned
		{"command substitution in a commit message", bash(`git commit -m "$(aws s3 ls)"`), true},
		{"backtick substitution in a commit message", bash("git commit -m \"`gcloud auth print-access-token`\""), true},
		{"message word also earlier", bash(`git commit -m "aws" ; aws s3 ls`), true},
		{"real expansion before an identical message", bash(`echo $AWS_PROFILE; git commit -m AWS_PROFILE`), true},
		{"real URL before an identical message", bash(`curl https://api.github.com/user; git commit -m https://api.github.com/user`), true},
		{"substitution in a grep pattern", bash(`grep "$(gcloud auth print-access-token)" f`), true},
		{"expansion in a grep pattern", bash(`grep "$GITHUB_TOKEN" f`), true},
		{"expandable here-string with a substitution", pwsh("@\"\n$(aws s3 ls)\n\"@ | Set-Content x"), true},
		{"single-quoted message stays text", bash(`git commit -m 'drop AWS_PROFILE=prod'`), false},
		{"double-quoted literal message stays text", bash(`git commit -m "see https://api.github.com/repos"`), false},
		{"single-quoted grep $ stays text", bash(`grep -rn '$AWS_PROFILE' src/`), false},
		{"literal here-string stays text", pwsh("@'\nos.Getenv(\"AWS_REGION\")\n'@ | Set-Content x"), false},
		// 2. docker run: every word after run is checked against images; --entrypoint too
		{"docker --platform", bash("docker run --platform linux/amd64 amazon/aws-cli s3 ls"), true},
		{"docker --pull", bash("docker run --pull always amazon/aws-cli s3 ls"), true},
		{"docker -u", bash("docker run -u 0 amazon/aws-cli s3 ls"), true},
		{"docker --network", bash("docker run --network host amazon/aws-cli s3 ls"), true},
		{"docker -w", bash("docker run -w /x amazon/aws-cli s3 ls"), true},
		{"docker --entrypoint aws", bash("docker run --entrypoint aws python:3 s3 ls"), true},
		{"docker --entrypoint=aws", bash("docker run --entrypoint=aws python:3 s3 ls"), true},
		{"docker in-scope image", bash("docker run --pull always mcr.microsoft.com/azure-cli az version"), false},
		{"docker ordinary image", bash("docker run --rm -u 0 node:20 npm test"), false},
		// 3. start / Start-Process targets
		{"start with a title", pwsh(`cmd /c start "title" aws s3 ls`), true},
		{"start /b with an empty title", pwsh(`cmd /c start /b "" aws s3 ls`), true},
		{"Start-Process after -WindowStyle", pwsh("Start-Process -WindowStyle Hidden aws"), true},
		{"Start-Process -WorkingDirectory then positional", pwsh(`Start-Process -WorkingDirectory C:\x aws -ArgumentList s3`), true},
		{"Start-Process -ArgumentList contents", pwsh("Start-Process cmd -ArgumentList '/c aws s3 ls'"), true},
		{"Start-Process -ArgumentList list", pwsh("Start-Process pwsh -ArgumentList '-c','gcloud projects list'"), true},
		{"Start-Process ordinary", pwsh("Start-Process notepad -ArgumentList 'notes.md' -WindowStyle Normal"), false},
		{"start ordinary", pwsh(`cmd /c start "Build" npm run build`), false},
	}
	for _, c := range cases {
		d := f.s.Evaluate(f.cwd, c.call)
		if (d.Action == Deny) != c.deny {
			t.Errorf("%s: %q deny=%v, want %v (%s)", c.name, c.call.Command, d.Action == Deny, c.deny, d.Reason())
		}
	}
}
