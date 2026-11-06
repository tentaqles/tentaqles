//go:build !windows

package trust

import "os"

func makeDirLink(target, link string) error { return os.Symlink(target, link) }
