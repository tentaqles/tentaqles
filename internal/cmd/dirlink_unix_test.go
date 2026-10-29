//go:build !windows

package cmd

import "os"

func makeDirLink(target, link string) error { return os.Symlink(target, link) }
