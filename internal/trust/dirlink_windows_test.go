//go:build windows

package trust

import "os/exec"

// makeDirLink links link to the folder target: a junction, which needs no
// privilege (a symlink does).
func makeDirLink(target, link string) error {
	return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run()
}
