package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// bundledTQPath looks for a tq binary next to the running executable
// (tq.exe on Windows, tq elsewhere), falling back to the TQ_BUNDLED_PATH
// env var. It returns "" when no bundled binary is found.
func bundledTQPath() string {
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		name := "tq"
		if runtime.GOOS == "windows" {
			name = "tq.exe"
		}
		candidate := filepath.Join(dir, name)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate
		}
	}
	if p := os.Getenv("TQ_BUNDLED_PATH"); p != "" {
		if info, statErr := os.Stat(p); statErr == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}
