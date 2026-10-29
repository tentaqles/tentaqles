//go:build windows

package cmd

import "os/exec"

func makeDirLink(target, link string) error {
	return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run()
}
