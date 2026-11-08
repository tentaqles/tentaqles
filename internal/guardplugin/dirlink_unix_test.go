//go:build !windows

package guardplugin

import "os"

func makeDirLink(target, link string) error { return os.Symlink(target, link) }
