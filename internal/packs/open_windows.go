//go:build windows

package packs

import "os"

// openNoBlock opens for reading (Windows has no FIFOs in the file system).
func openNoBlock(p string) (*os.File, error) { return os.Open(p) }
