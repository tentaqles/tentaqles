package guard

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// C3 / R3: every ordinary spelling of the git command word is recognised.
func TestGitInvocations_CommandWordForms(t *testing.T) {
	for _, c := range []string{
		`git commit -m "feat: seed"`,
		`git.exe commit -m "feat: seed"`,
		`GIT.EXE commit -m "feat: seed"`,
		`/usr/bin/git commit -m "feat: seed"`,
		`/mingw64/bin/git.exe commit -m "feat: seed"`,
		`C:\Program\Git\cmd\git.exe commit -m "feat: seed"`,
		`"C:\Program Files\Git\cmd\git.exe" commit -m "feat: seed"`,
		`"git" commit -m "feat: seed"`,
		`'git' commit -m "feat: seed"`,
		`command git commit -m "feat: seed"`,
		`env git commit -m "feat: seed"`,
		`env GIT_AUTHOR_DATE=now git commit -m "feat: seed"`,
		`env -u FOO A=1 B=2 git commit -m "feat: seed"`,
		`GIT_AUTHOR_DATE=now git commit -m "feat: seed"`,
		`A=1 B=2 git commit -m "feat: seed"`,
		`exec git commit -m "feat: seed"`,
		`nohup git commit -m "feat: seed"`,
		`time git commit -m "feat: seed"`,
		`sudo -u bob git commit -m "feat: seed"`,
		`nice -n 5 git commit -m "feat: seed"`,
		`& git commit -m "feat: seed"`,
		`& "git.exe" commit -m "feat: seed"`,
		`git --git-dir=.git commit -m "feat: seed"`,
		`git --git-dir .git commit -m "feat: seed"`,
		`git --git-dir=.git --work-tree=. commit -m "feat: seed"`,
		`git --git-dir .git --work-tree . commit -m "feat: seed"`,
		`git --namespace x --super-prefix y/ commit -m "feat: seed"`,
		`git -C repo -c core.x=1 --no-pager commit -m "feat: seed"`,
		`echo hi && GIT_X=1 git commit -m "feat: seed"`,
	} {
		invs := GitInvocations(c)
		found := false
		for _, inv := range invs {
			if inv.Sub == "commit" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no git commit recognised (got %+v)", c, invs)
		}
	}
	for _, c := range []string{
		`gitk commit`,
		`legit commit`,
		`/usr/bin/gitx commit`,
	} {
		for _, inv := range GitInvocations(c) {
			if inv.Sub == "commit" {
				t.Errorf("%s: wrongly recognised as git commit", c)
			}
		}
	}
}

func TestGitInvocations_QuoteAwareArgs(t *testing.T) {
	invs := GitInvocations(`git commit -m "feat(api): x; y" -- export.csv "my file.csv"`)
	if len(invs) != 1 {
		t.Fatalf("invs = %+v", invs)
	}
	want := []string{"-m", "feat(api): x; y", "--", "export.csv", "my file.csv"}
	if !reflect.DeepEqual(invs[0].Args, want) {
		t.Fatalf("args = %q, want %q", invs[0].Args, want)
	}
	// a command hidden inside a quoted string is still found (and marked)
	invs = GitInvocations(`sh -c 'git add -A && git commit -m x'`)
	hidden := false
	for _, inv := range invs {
		if inv.Sub == "commit" && inv.Hidden {
			hidden = true
		}
	}
	if !hidden {
		t.Fatalf("quoted commit not found as hidden: %+v", invs)
	}
}

func TestGitInvocations_GitDirOverride(t *testing.T) {
	for _, c := range []string{
		`git --git-dir=.git commit -m x`,
		`git --work-tree . commit -m x`,
		`GIT_DIR=x git commit -m x`,
		`env GIT_WORK_TREE=y git commit -m x`,
	} {
		invs := GitInvocations(c)
		if len(invs) != 1 || !invs[0].GitDirOverride {
			t.Errorf("%s: %+v", c, invs)
		}
	}
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// C1 / R1: the directory a git call acts on.
func TestGitInvocation_Dir(t *testing.T) {
	cwd := t.TempDir()
	wt := mkdir(t, cwd, "wt")
	sub := mkdir(t, wt, "sub")
	spaced := mkdir(t, cwd, "my wt")
	same := func(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }
	cases := []struct {
		cmd  string
		want string
	}{
		{`git commit -m x`, cwd},
		{`git -C wt commit -m x`, wt},
		{`git -C wt -C sub commit -m x`, sub},
		{`cd wt && git commit -m x`, wt},
		{`cd wt; git commit -m x`, wt},
		{`cd "wt" && git commit -m x`, wt},
		{`cd 'my wt' && git commit -m x`, spaced},
		{`cd "` + wt + `" && git commit -m x`, wt},
		{`cd ` + filepath.ToSlash(wt) + ` && git commit -m x`, wt},
		{`cd wt && git -C sub commit -m x`, sub},
		{`Set-Location wt; git commit -m x`, wt},
		{`Set-Location -Path wt; git commit -m x`, wt},
		{`pushd wt && git commit -m x`, wt},
		{`cd wt && git add -A && git commit -m x`, wt},
	}
	if runtime.GOOS == "windows" {
		// Git Bash spelling of an absolute Windows path
		vol := strings.ToLower(filepath.VolumeName(wt)[:1])
		msys := "/" + vol + filepath.ToSlash(wt[len(filepath.VolumeName(wt)):])
		cases = append(cases, struct{ cmd, want string }{`cd ` + msys + ` && git commit -m x`, wt})
		cases = append(cases, struct{ cmd, want string }{`git -C ` + msys + ` commit -m x`, wt})
	}
	for _, c := range cases {
		var got string
		var err error
		for _, inv := range GitInvocations(c.cmd) {
			if inv.Sub == "commit" {
				got, err = inv.Dir(cwd)
			}
		}
		if err != nil || !same(got, c.want) {
			t.Errorf("%s: Dir = %q, %v; want %q", c.cmd, got, err, c.want)
		}
	}
	for _, c := range []string{
		`git -C missing commit -m x`,
		`cd missing && git commit -m x`,
		`cd $HOME/x && git commit -m x`,
		`cd ~/x && git commit -m x`,
		`cd && git commit -m x`,
		`cd - && git commit -m x`,
		`cd "$(mktemp -d)" && git commit -m x`,
		`git -C "$DIR" commit -m x`,
		`echo hi && cd wt && git commit -m x`, // a cd that is not the first segment
		`cd wt && cd sub && git commit -m x`,
		`(cd wt) && git commit -m x`,
		`git --git-dir=wt/.git commit -m x`,
		`GIT_DIR=wt/.git git commit -m x`,
		`sh -c 'cd wt && git commit -m x'`,
	} {
		for _, inv := range GitInvocations(c) {
			if inv.Sub != "commit" {
				continue
			}
			if d, err := inv.Dir(cwd); err == nil {
				t.Errorf("%s: want an unresolvable directory, got %q", c, d)
			}
		}
	}
}

func TestBaseDir(t *testing.T) {
	cwd := t.TempDir()
	wt := mkdir(t, cwd, "app")
	if d, err := BaseDir(cwd, `cd app && cp a b`); err != nil || !strings.EqualFold(d, wt) {
		t.Fatalf("BaseDir = %q %v", d, err)
	}
	if d, err := BaseDir(cwd, `cp a b`); err != nil || d != cwd {
		t.Fatalf("BaseDir = %q %v", d, err)
	}
	if _, err := BaseDir(cwd, `cd nowhere && cp a b`); err == nil {
		t.Fatal("missing dir must be an error")
	}
}

func TestInlineIdentityOverride_WrappedForms(t *testing.T) {
	for _, c := range []string{
		`env git -c user.email=x@y.z commit -m x`,
		`"git" -c user.email=x@y.z commit -m x`,
	} {
		if !HasInlineIdentityOverride(c) {
			t.Errorf("%q should be an inline identity override", c)
		}
	}
}

// Parser differential: shell spellings that run git commit are recognised the
// same way here and in commitmsg (see commitmsg TestCheck_ParserParity).
func TestGitInvocations_ShellSpellings(t *testing.T) {
	for _, c := range []string{
		`git "comm"it -m x`,
		`git 'commit' -m x`,
		`git $'commit' -m x`,
		`git $"commit" -m x`,
		`g\it commit -m x`,
		`git c\ommit -m x`,
		`git commit;`,
		`git commit|cat`,
		`git commit&&true`,
		`true||git commit`,
		"echo $(git commit -m x)",
		"echo `git commit -m x`",
		`echo "$(git commit -m x)"`,
	} {
		found := false
		for _, inv := range GitInvocations(c) {
			if inv.Sub == "commit" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: commit not recognised", c)
		}
	}
}

// Round 2 (N2): per-wrapper flags, nested shells and the hidden-git net.
func TestGitInvocations_Round2Wrappers(t *testing.T) {
	for c, hidden := range map[string]bool{
		`time -p git commit -m x`:                                    false,
		`command -p git commit -m x`:                                 false,
		`command -v git commit -m x`:                                 false,
		`exec -a name git commit -m x`:                               false,
		`exec -c -l git commit -m x`:                                 false,
		`timeout 30 git commit -m x`:                                 false,
		`timeout -s KILL -k 5 30s git commit -m x`:                   false,
		`timeout --signal=TERM --preserve-status 1m git commit -m x`: false,
		`xargs git commit -m x`:                                      false,
		`xargs -0 -n 1 -I {} git commit -m x`:                        false,
		`sudo -u bob -E git commit -m x`:                             false,
		`nice -n 5 nohup env A=1 git commit -m x`:                    false,
		`bash -c "git commit -m seed"`:                               true,
		`sh -c 'git commit -m "feat: seed"'`:                         true,
		`bash -lc "git commit -m seed"`:                              true,
		`zsh -c "git commit"`:                                        true,
		`pwsh -Command "git commit -m seed"`:                         true,
		`powershell -c "git commit -m seed"`:                         true,
		`cmd /c "git commit -m seed"`:                                true,
		`iex "git commit -m seed"`:                                   true,
		`Invoke-Expression 'git commit -m seed'`:                     true,
		`Start-Process git -ArgumentList 'commit','-m','seed' -Wait`: true,
		`echo git commit`:                                            true,
		`foo --run git commit`:                                       true,
	} {
		var got *GitInvocation
		for _, inv := range GitInvocations(c) {
			if inv.Sub == "commit" {
				inv := inv
				got = &inv
			}
		}
		if got == nil {
			t.Errorf("%s: no commit found", c)
			continue
		}
		if got.Hidden != hidden {
			t.Errorf("%s: Hidden=%v, want %v", c, got.Hidden, hidden)
		}
	}
	// the naive pass strips quotes from a hidden sub-command
	for _, inv := range GitInvocations(`git commit -m "a; git push"`) {
		if strings.ContainsAny(inv.Sub, `"'`) {
			t.Errorf("sub keeps a quote: %q", inv.Sub)
		}
	}
	// prose inside one quoted word is not a git call
	for _, c := range []string{`grep -r "git commit" docs`, `echo "run git push later"`} {
		if invs := GitInvocations(c); len(invs) != 0 {
			t.Errorf("%s: %+v", c, invs)
		}
	}
}
