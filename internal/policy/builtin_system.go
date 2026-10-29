package policy

// The system rules of spec E §5.3 (category system). tq/outside-path-write
// needs the workspace roots, so the hook computes it (internal/cmd,
// claudehook_system.go); the two below are regex heuristics like every
// shell rule.

// cmdStart is where a command word may start: the input start or a
// separator, optionally behind a quote opening `sh -c "…"`, and behind
// prefix words that run the next word (sudo, env, nohup, time, command,
// exec).
const cmdStart = `(^|[;&|\n(` + bt + `]|\s-[a-z]*c\s+["'])\s*((sudo|doas|env|nohup|time|command|exec)\s+(-\S+\s+)*)*`

// sysDir is a system folder at the start of a path (any quote first):
// Windows (drive or Git Bash /c/ form) or POSIX.
const sysDir = `["']?(([a-z]:[/\\]+|/[a-z]/)(windows|program\s?files(\s\(x86\))?|programdata)|/(etc|usr|bin|sbin|boot|system|library))([/\\"'\s]|$)`

// sysDirPath is sysDir for a normalized (slash) file-tool path.
const sysDirPath = `^(([a-z]:/+|/[a-z]/)(windows|program files( \(x86\))?|programdata)|/(etc|usr|bin|sbin|boot|system|library))(/|$)`

// sysWriteVerbs write, delete, move or change what they name.
const sysWriteVerbs = `\b(tee|sed\s+-i|set-content|add-content|out-file|sc|ac|new-item|ni|rm|rmdir|del|erase|rd|remove-item|ri|mv|move|move-item|mi|rename-item|ren|mkdir|md|chmod|chown|touch|ln|install|truncate)\b`

func init() {
	RegisterBuiltin(
		Rule{
			ID: "tq/system-dir-write", Action: Deny,
			Tool:   "Edit|Write|MultiEdit|NotebookEdit",
			Path:   sysDirPath,
			Reason: "writing under a system folder (C:\\Windows, C:\\Program Files, C:\\ProgramData, /etc, /usr, /bin, /sbin, /boot, /System, /Library) is done by hand, not by an agent",
		},
		Rule{
			ID: "tq/system-dir-write", Action: Deny,
			// A redirect or a writing verb naming a system folder anywhere in
			// its command; a copy only when the folder is the destination (the
			// last word), so `cp /etc/hosts ./bak` is a read.
			Command: `>{1,2}\s*` + sysDir +
				`|` + sysWriteVerbs + seg + `\s` + sysDir +
				`|\b(cp|copy|copy-item|cpi|xcopy|robocopy)\b` + seg + `\s` + sysDir + `[^\s;&|\n]*` + destTail,
			SkipHeredocData: true,
			Reason:          "writing or deleting under a system folder (C:\\Windows, C:\\Program Files, C:\\ProgramData, /etc, /usr, /bin, /sbin, /boot, /System, /Library) is done by hand, not by an agent",
		},
		Rule{
			ID: "tq/elevation", Action: Ask,
			Command: `(^|[;&|\n(` + bt + `]|\s-[a-z]*c\s+["'])\s*["']?(\S*[/\\])?(sudo|doas|pkexec|gsudo|runas)(\.exe)?` + verbEnd +
				`|(^|[;&|\n(` + bt + `])\s*su\s+([^;&|\n]*\s)?-c\b` +
				`|\b(start-process|saps)\b` + seg + `-verb\s+["']?runas\b`,
			SkipHeredocData: true,
			Reason:          "this runs a command with elevated rights (sudo, doas, su -c, runas, gsudo, Start-Process -Verb RunAs, pkexec)",
		},
	)
}
