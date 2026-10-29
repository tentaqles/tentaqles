package policy

import "testing"

// nestedAgentRule returns the compiled tq/nested-agent-loosened rule.
func nestedAgentRule(t *testing.T) *Rule {
	t.Helper()
	for _, r := range builtinSet(t).Rules {
		if r.ID == NestedAgentRuleID {
			return r
		}
	}
	t.Fatalf("%s is not built in", NestedAgentRuleID)
	return nil
}

// tq/nested-agent-loosened (spec E §9) denies an agent's shell call that
// starts another agent, or changes the env for one, in off mode or with
// loosened settings. A plain nested `claude -p` or `codex exec` is allowed.
func TestNestedAgentLoosened(t *testing.T) {
	r := nestedAgentRule(t)
	if r.Action != Deny {
		t.Fatalf("action = %q, want deny", r.Action)
	}
	deny := []ToolCall{
		// TQ_MODE assignments to anything but default or strict, and TQ_ENABLED=0
		{Tool: "Bash", Command: "TQ_MODE=off claude -p hi"},
		{Tool: "Bash", Command: "TQ_MODE=relaxed git push --force"},
		{Tool: "Bash", Command: "FOO=1 TQ_MODE=custom claude"},
		{Tool: "Bash", Command: "export TQ_MODE=off"},
		{Tool: "Bash", Command: "cd x && export TQ_MODE='relaxed' && claude"},
		{Tool: "Bash", Command: "env TQ_MODE=off claude -p x"},
		{Tool: "Bash", Command: "env -i TQ_MODE=relaxed codex exec x"},
		{Tool: "Bash", Command: "set -x TQ_MODE off"},
		{Tool: "Bash", Command: "set -gx TQ_MODE relaxed; claude"},
		{Tool: "Bash", Command: "TQ_ENABLED=0 claude"},
		{Tool: "Bash", Command: "export TQ_ENABLED=0"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE='off'; claude"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = \"relaxed\""},
		{Tool: "PowerShell", Command: "$env:TQ_ENABLED = '0'"},
		{Tool: "PowerShell", Command: "[Environment]::SetEnvironmentVariable('TQ_MODE', 'off')"},
		{Tool: "PowerShell", Command: "Set-Item Env:TQ_MODE off"},
		{Tool: "PowerShell", Command: "cmd /c \"set TQ_MODE=off && claude\""},
		// off launchers and loosening tq verbs
		{Tool: "Bash", Command: "claude-off -p hi"},
		{Tool: "Bash", Command: "codex-off exec x"},
		{Tool: "Bash", Command: "cd /x; claude-off"},
		{Tool: "Bash", Command: "tq off"},
		{Tool: "PowerShell", Command: "& 'C:\\Users\\u\\bin\\tq.exe' off"},
		{Tool: "Bash", Command: "tq run --off -- claude"},
		{Tool: "Bash", Command: "/usr/local/bin/tq run acme --off -- codex"},
		{Tool: "Bash", Command: "tq env --off --shell bash"},
		{Tool: "Bash", Command: "eval \"$(tq env --shell bash --off)\""},
		{Tool: "Bash", Command: "tq run acme --tq-mode relaxed -- claude -p x"},
		{Tool: "Bash", Command: "tq run . --tq-mode=off -- claude"},
		{Tool: "Bash", Command: "tq run . -- claude --tq-mode custom -p x"},
		{Tool: "Bash", Command: "claude --tq-mode relaxed -p x"},
		// claude flags and env
		{Tool: "Bash", Command: "claude --dangerously-skip-permissions -p x"},
		{Tool: "Bash", Command: "claude -p x --allow-dangerously-skip-permissions"},
		{Tool: "Bash", Command: "claude --permission-mode bypassPermissions -p x"},
		{Tool: "Bash", Command: "claude --permission-mode=auto -p x"},
		{Tool: "Bash", Command: "claude -p x --permission-mode dontAsk"},
		{Tool: "Bash", Command: "claude --bare -p x"},
		{Tool: "Bash", Command: "claude --safe-mode"},
		{Tool: "Bash", Command: "claude --setting-sources project,local -p x"},
		{Tool: "Bash", Command: "claude --setting-sources=project -p x"},
		{Tool: "Bash", Command: `claude --settings '{"enabledPlugins":{"tentaqles@tentaqles":false}}' -p x`},
		{Tool: "Bash", Command: `claude -p x --settings '{"disableAllHooks":true}'`},
		{Tool: "Bash", Command: `claude --settings '{"hooks": {}}' -p x`},
		// inline settings that switch tq off or loosen the mode (final review M2)
		{Tool: "Bash", Command: `claude --settings '{"env":{"TQ_MODE":"off"}}' -p x`},
		{Tool: "Bash", Command: `claude --settings '{"env": {"TQ_ENABLED": "0"}}' -p x`},
		{Tool: "Bash", Command: `claude --settings '{"env":{"CLAUDE_CONFIG_DIR":"/x"}}' -p x`},
		{Tool: "Bash", Command: `claude --settings '{"env":{"CLAUDE_CODE_SIMPLE":"1"}}' -p x`},
		{Tool: "Bash", Command: `claude --settings '{"env":{"CLAUDE_CODE_SAFE_MODE":"1"}}' -p x`},
		{Tool: "Bash", Command: `claude --settings '{"permissions":{"defaultMode":"bypassPermissions"}}' -p x`},
		{Tool: "Bash", Command: `claude --settings '{"permissions":{"defaultMode":"auto"}}' -p x`},
		{Tool: "Bash", Command: `claude --settings='{"permissions":{"defaultMode":"dontAsk"}}' -p x`},
		{Tool: "Bash", Command: "claude --settings /tmp/loose.json -p x"},
		{Tool: "PowerShell", Command: "& 'C:\\Users\\u\\AppData\\Roaming\\npm\\claude.cmd' --dangerously-skip-permissions"},
		{Tool: "Bash", Command: "tq run acme -- claude --dangerously-skip-permissions -p x"},
		{Tool: "Bash", Command: "nohup claude --dangerously-skip-permissions -p x &"},
		{Tool: "Bash", Command: "CLAUDE_CODE_SIMPLE=1 claude -p x"},
		{Tool: "Bash", Command: "export CLAUDE_CODE_SAFE_MODE=1"},
		{Tool: "PowerShell", Command: "$env:CLAUDE_CODE_SIMPLE = '1'; claude -p x"},
		// clearing or reassigning the agent's config home
		{Tool: "Bash", Command: "env -u CLAUDE_CONFIG_DIR claude -p x"},
		{Tool: "Bash", Command: "env --unset=CODEX_HOME codex exec x"},
		{Tool: "Bash", Command: "unset CLAUDE_CONFIG_DIR"},
		{Tool: "Bash", Command: "unset FOO CODEX_HOME && codex"},
		{Tool: "Bash", Command: "CLAUDE_CONFIG_DIR= claude -p x"},
		{Tool: "Bash", Command: "CLAUDE_CONFIG_DIR=~/.claude claude -p x"},
		{Tool: "Bash", Command: "export CODEX_HOME=$HOME/.codex"},
		{Tool: "Bash", Command: "set -e CLAUDE_CONFIG_DIR"},
		{Tool: "PowerShell", Command: "Remove-Item Env:CLAUDE_CONFIG_DIR; claude -p x"},
		{Tool: "PowerShell", Command: "Remove-Item -Path Env:\\CODEX_HOME"},
		{Tool: "PowerShell", Command: "$env:CLAUDE_CONFIG_DIR = \"$HOME\\.claude\""},
		// codex flags
		{Tool: "Bash", Command: "codex --dangerously-bypass-approvals-and-sandbox exec x"},
		{Tool: "Bash", Command: "codex --yolo"},
		{Tool: "Bash", Command: "codex -a never exec x"},
		{Tool: "Bash", Command: "codex --ask-for-approval=never"},
		{Tool: "Bash", Command: "codex -s danger-full-access exec x"},
		{Tool: "Bash", Command: "codex exec --sandbox danger-full-access x"},
		{Tool: "Bash", Command: "codex --disable hooks"},
		{Tool: "Bash", Command: "codex -c features.hooks=false exec x"},
		{Tool: "Bash", Command: "codex exec --ignore-rules x"},
		{Tool: "Bash", Command: "codex --dangerously-bypass-hook-trust"},
		{Tool: "PowerShell", Command: "codex.exe --yolo"},
		// a harmless TQ_MODE assignment never hides the loosened launch after it
		{Tool: "PowerShell", Command: "$env:TQ_MODE='default'; claude --dangerously-skip-permissions -p x"},
		{Tool: "PowerShell", Command: "${env:TQ_MODE} = 'strict'; claude --bare -p x"},
		{Tool: "Bash", Command: "export TQ_MODE=strict; claude-off -p x"},
		{Tool: "Bash", Command: "declare -x TQ_MODE=default; claude --dangerously-skip-permissions"},
		{Tool: "Bash", Command: "set -x TQ_MODE strict; codex --yolo"},
		{Tool: "Bash", Command: "TQ_MODE=default; tq off"},
		{Tool: "Bash", Command: "export TQ_MODE=strict && TQ_MODE=off claude"},
		{Tool: "Bash", Command: "export TQ_MODE=strict|claude --dangerously-skip-permissions"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE='strict'; $env:CLAUDE_CONFIG_DIR=$null; claude"},
		{Tool: "Bash", Command: "TQ_MODE=defaults claude"},
		{Tool: "Bash", Command: "TQ_MODE=strictly claude"},
		{Tool: "Bash", Command: "TQ_MODE=default-off claude"},
		{Tool: "Bash", Command: "TQ_MODE=\"default off\" claude"},
		// r1 Major 1: an empty quoted token joined to a word is that word
		{Tool: "Bash", Command: "TQ_MODE=''off claude -p x"},
		{Tool: "Bash", Command: `TQ_MODE=""off claude -p x`},
		{Tool: "Bash", Command: `export TQ_MODE=''"off"`},
		{Tool: "Bash", Command: "TQ_MODE=default''x claude"},
		{Tool: "Bash", Command: "TQ_MODE='strict'off claude"},
		{Tool: "Bash", Command: "TQ_MODE=strict\\ off claude"},
		{Tool: "Bash", Command: "set -x TQ_MODE ''off"},
		{Tool: "Bash", Command: "TQ_MODE=$(echo off) claude"},
		{Tool: "Bash", Command: "TQ_MODE=`echo off` claude"},
		{Tool: "Bash", Command: "TQ_MODE=$M claude"},
		{Tool: "Bash", Command: "TQ_MODE=<x claude"},
		{Tool: "Bash", Command: "TQ_MODE=Default claude"},
		{Tool: "Bash", Command: "TQ_MODE=STRICT claude"},
		// r1 Major 1: PowerShell expressions
		{Tool: "PowerShell", Command: "$env:TQ_MODE = ('off'); claude"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict' -replace 'strict','off'; claude"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE='strict'.Replace('strict','off'); claude"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'de' + 'fault'"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict'+'x'"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = ''+'off'"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = \"$m\""},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = $m"},
		{Tool: "PowerShell", Command: "${env:TQ_MODE} = (Get-Content m.txt)"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = $null"},
		{Tool: "PowerShell", Command: "cmd /c set \"TQ_MODE=strict\"x"},
		{Tool: "PowerShell", Command: "cmd /c \"set TQ_MODE=&& claude\""},
		// r1 Minor 1: emptying or removing TQ_MODE drops a strict launch layer
		{Tool: "PowerShell", Command: "$env:TQ_MODE = ''; claude -p x"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE=\"\""},
		{Tool: "PowerShell", Command: "$env:TQ_MODE ="},
		{Tool: "Bash", Command: "TQ_MODE= claude -p x"},
		{Tool: "Bash", Command: "export TQ_MODE="},
		{Tool: "Bash", Command: "TQ_MODE='' claude"},
		{Tool: "Bash", Command: "unset TQ_MODE"},
		{Tool: "Bash", Command: "unset -v TQ_MODE; claude"},
		{Tool: "Bash", Command: "env -u TQ_MODE claude -p x"},
		{Tool: "Bash", Command: "set -e TQ_MODE"},
		{Tool: "Bash", Command: "set --erase TQ_MODE"},
		{Tool: "PowerShell", Command: "Remove-Item Env:TQ_MODE"},
		{Tool: "PowerShell", Command: "Remove-Item -Path Env:\\TQ_MODE; claude"},
		// r2 Major 1: in PowerShell the right-hand side is a whole pipeline
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict' | Out-Null"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict' | ForEach-Object { 'off' }; claude"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE='default'|%{'off'}"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict' &"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict'\n| ForEach-Object { 'off' }"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict'\r\n    | ForEach-Object { 'off' }"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict' # c\n\n  # more\n  | ForEach-Object { 'off' }"},
		// r2 Minor 1: trailing words and command output after the safe word
		{Tool: "PowerShell", Command: "cmd /c \"set TQ_MODE=strict & claude\""},
		{Tool: "PowerShell", Command: "cmd /c set TQ_MODE=strict foo"},
		{Tool: "Bash", Command: "set -x TQ_MODE strict off"},
		{Tool: "Bash", Command: "set -gx TQ_MODE default x; claude"},
		{Tool: "Bash", Command: "TQ_MODE=strict`echo x` claude"},
		{Tool: "Bash", Command: "export TQ_MODE=default`printf off`"},
		// wrappers and VAR=value prefixes before tq
		{Tool: "Bash", Command: "FOO=1 tq run --off -- claude"},
		{Tool: "Bash", Command: "env tq off"},
		{Tool: "Bash", Command: "nohup tq env --off --shell bash"},
		{Tool: "Bash", Command: "cd x && env -i tq run . --tq-mode relaxed -- claude"},
	}
	for _, c := range deny {
		if !r.Match(c) {
			t.Errorf("%s %q: want %s to match", c.Tool, c.Command, NestedAgentRuleID)
		}
	}
	allow := []ToolCall{
		{Tool: "Bash", Command: `claude -p "summarise the diff"`},
		{Tool: "Bash", Command: "codex exec 'fix the test'"},
		{Tool: "Bash", Command: "echo TQ_MODE"},
		{Tool: "Bash", Command: "echo $TQ_MODE"},
		{Tool: "Bash", Command: `git commit -m "docs: TQ_MODE=off"`},
		{Tool: "Bash", Command: "git commit -m \"$(cat <<'EOF'\ndocs: claude-off and tq off\n\nTQ_MODE=off claude --dangerously-skip-permissions\nEOF\n)\""},
		{Tool: "Bash", Command: `git commit -m "describe tq off and claude-off"`},
		{Tool: "Bash", Command: "TQ_MODE=strict claude -p x"},
		{Tool: "Bash", Command: "export TQ_MODE=default"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict'; claude -p x"},
		{Tool: "Bash", Command: "set -x TQ_MODE strict"},
		{Tool: "Bash", Command: "export TQ_MODE=strict; claude -p x"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE='default'; claude -p x"},
		{Tool: "Bash", Command: "TQ_MODE='strict' codex exec x"},
		{Tool: "Bash", Command: "(TQ_MODE=strict claude -p x)"},
		{Tool: "Bash", Command: "FOO=1 tq doctor"},
		{Tool: "Bash", Command: "env tq mode"},
		{Tool: "PowerShell", Command: "cmd /c set \"TQ_MODE=strict\""},
		{Tool: "PowerShell", Command: "cmd /c \"set TQ_MODE=default&& claude -p x\""},
		{Tool: "PowerShell", Command: "cmd /c \"set TQ_MODE=strict\""},
		// exact default/strict followed by a word or statement end
		{Tool: "PowerShell", Command: "$env:TQ_MODE  =  \"strict\"  ; claude -p x"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'default' # back to default"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict'\nclaude -p x"},
		{Tool: "PowerShell", Command: "if ($x) { $env:TQ_MODE = 'strict' }"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict'\n\nclaude -p x"},
		{Tool: "PowerShell", Command: "$env:TQ_MODE = 'strict' # set it\nclaude | Out-File log.txt"},
		{Tool: "PowerShell", Command: "($env:TQ_MODE = 'strict') | Out-Null"},
		{Tool: "Bash", Command: "set -x TQ_MODE strict ; claude -p x"},
		{Tool: "Bash", Command: "set -x TQ_MODE strict\nclaude -p x"},
		{Tool: "Bash", Command: "set -x TQ_MODE strict | cat"},
		{Tool: "Bash", Command: "TQ_MODE=strict claude -p x | tee log"},
		{Tool: "PowerShell", Command: "cmd /c \"set TQ_MODE=strict& claude -p x\""},
		{Tool: "PowerShell", Command: "cmd /c \"(set TQ_MODE=default) && claude -p x\""},
		{Tool: "Bash", Command: "TQ_MODE=\"strict\" claude -p x"},
		{Tool: "Bash", Command: "export TQ_MODE=default\nclaude -p x"},
		{Tool: "Bash", Command: "TQ_MODE=strict"},
		{Tool: "Bash", Command: "x=$(TQ_MODE=strict claude -p x)"},
		{Tool: "Bash", Command: "set -x TQ_MODE  'strict'; claude -p x"},
		{Tool: "Bash", Command: "TQ_MODE=strict&&claude -p x"},
		{Tool: "Bash", Command: "echo \"$TQ_MODE\""},
		{Tool: "Bash", Command: "test \"$TQ_MODE\" = off"},
		{Tool: "Bash", Command: "set -q TQ_MODE"},
		{Tool: "Bash", Command: "TQ_ENABLED=1 tq doctor"},
		{Tool: "Bash", Command: "claude --permission-mode acceptEdits -p x"},
		{Tool: "Bash", Command: "claude --permission-mode plan -p x"},
		{Tool: "Bash", Command: "claude --setting-sources user,project -p x"},
		{Tool: "Bash", Command: `claude --settings '{"permissions":{"allow":["Bash(npm test)"]}}' -p x`},
		{Tool: "Bash", Command: "tq run acme -- claude -p x"},
		{Tool: "Bash", Command: "tq run . --tq-mode strict -- claude -p x"},
		{Tool: "Bash", Command: "claude --tq-mode strict -p x"},
		{Tool: "Bash", Command: "tq env --shell bash"},
		{Tool: "Bash", Command: "tq mode"},
		{Tool: "Bash", Command: "tq doctor --json"},
		{Tool: "Bash", Command: "codex -a on-request -s workspace-write exec x"},
		{Tool: "Bash", Command: "echo $CLAUDE_CONFIG_DIR"},
		{Tool: "Bash", Command: "ls \"$CODEX_HOME/rules\""},
		{Tool: "PowerShell", Command: "Get-ChildItem Env:CLAUDE_CONFIG_DIR"},
		{Tool: "Bash", Command: "grep -rn claude-off docs/"},
		{Tool: "Bash", Command: "go test ./internal/cmd -run TestRunOff"},
		{Tool: "Read", Path: "docs/claude-off.md"},
		{Tool: "Write", Path: "notes.md", Content: "TQ_MODE=off claude-off"},
	}
	for _, c := range allow {
		if r.Match(c) {
			t.Errorf("%s %q: %s must not match", c.Tool, c.Command, NestedAgentRuleID)
		}
	}
}
