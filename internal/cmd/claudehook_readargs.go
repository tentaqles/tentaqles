package cmd

import (
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// readArgWriteRe is any command word that may write a file: a write verb, a
// shell or interpreter, a downloader, a wrapper that runs another command, or
// a tool that writes as a side effect. The left boundary includes / (after
// normShell, \ is /), so a program called by its path (/usr/bin/tee,
// C:\Windows\System32\cmd.exe) matches by its basename.
var readArgWriteRe = regexp.MustCompile(`(^|[\s;&|(/])(tee|cp|mv|rm|ln|mklink|dd|install|rsync|scp|sftp|ftp|touch|truncate|chmod|chown|shred|unlink|rmdir|mkdir|tar|unzip|zip|7z|sed|awk|perl|python3?|py|node|deno|bun|ruby|php|pwsh|powershell|bash|sh|zsh|cmd|sqlite3|del|erase|copy|move|ren|xcopy|robocopy|set-content|add-content|out-file|new-item|copy-item|move-item|remove-item|rename-item|clear-content|sc|ac|cpi|mi|ri|ni|iex|invoke-expression|start-process|eval|exec|xargs|find|curl|wget|invoke-webrequest|iwr|invoke-restmethod|irm|certutil|bitsadmin|start-bitstransfer|env|nohup|sudo|doas|runas|start|time|timeout|nice|watch|git|make|docker|podman|kubectl|expand-archive|compress-archive)(\.exe|\.cmd|\.bat)?($|[\s;&|)])`)

// programName is a command word's program: quotes stripped, the basename,
// lower case, without .exe, .cmd or .bat.
func programName(w string) string {
	w = strings.ToLower(strings.Trim(w, `"'`))
	w = path.Base(strings.ReplaceAll(w, `\`, "/"))
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		w = strings.TrimSuffix(w, ext)
	}
	return w
}

// withoutReadArgs returns cmd with the one stored plan a pack guard verified
// for this call (packs.GuardCtx.ReadArgs) replaced by a placeholder, so
// tq/hub-store-write checks everything else as usual. ok is false, and
// nothing is exempt, when:
//   - the command could write (a redirect, a substitution, or a word of
//     readArgWriteRe anywhere);
//   - verified paths occur other than exactly once in the whole command;
//   - the segment holding it does not start with the program the guard
//     declared (after path normalization: env tofu or bash -c are not tofu).
//
// A token matches only when it is the path itself (quotes stripped) or a
// flag's =value, spelled as the absolute path or its final path; no
// variable, glob, ~ or .. spelling.
func withoutReadArgs(cmd string, readArgs []packs.ReadArg) (string, bool) {
	if strings.ContainsAny(cmd, "<>`") || strings.Contains(cmd, "$(") || strings.Contains(cmd, "@(") {
		return cmd, false
	}
	if readArgWriteRe.MatchString(normShell(cmd)) {
		return cmd, false
	}
	program := map[string]string{} // path form -> program
	for _, a := range readArgs {
		if a.Program == "" {
			continue
		}
		program[normPathForm(a.Path)] = a.Program
		if fp, err := packs.FinalPath(a.Path); err == nil {
			program[normPathForm(fp)] = a.Program
		}
	}
	s := strings.NewReplacer("\r", " ", "\n", " ; ").Replace(cmd) // a newline separates segments
	isSep := func(c byte) bool { return c == ';' || c == '&' || c == '|' }
	isBlank := func(c byte) bool { return c == ' ' || c == '\t' || c == '\f' || c == '\v' }
	var b strings.Builder
	matches, okSeg := 0, false
	segFirst, atStart := "", true
	for i := 0; i < len(s); {
		c := s[i]
		if isSep(c) {
			atStart = true
			b.WriteByte(c)
			i++
			continue
		}
		if isBlank(c) {
			b.WriteByte(c)
			i++
			continue
		}
		j := i
		for j < len(s) && !isSep(s[j]) && !isBlank(s[j]) {
			j++
		}
		tok := s[i:j]
		first := atStart
		if atStart {
			segFirst, atStart = tok, false
		}
		prefix, val := "", strings.Trim(tok, `"'`)
		if strings.HasPrefix(val, "-") {
			if k := strings.Index(val, "="); k > 0 {
				prefix, val = val[:k+1], strings.Trim(val[k+1:], `"'`)
			}
		}
		if prog, hit := program[normPathForm(val)]; hit && val != "" && !strings.ContainsAny(val, "$%*?~") && !strings.Contains(val, "..") {
			matches++
			okSeg = !first && programName(segFirst) == prog
			b.WriteString(prefix + "tq-read-arg")
		} else {
			b.WriteString(tok)
		}
		i = j
	}
	if matches != 1 || !okSeg {
		return cmd, false
	}
	return b.String(), true
}
