package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// bundledTQPath looks for a tq binary bundled with the app:
//
//   - next to the running executable (tq.exe on Windows, tq elsewhere) —
//     this covers the Windows installer's $INSTDIR and the macOS bundle's
//     Contents/MacOS directory;
//   - on Linux, $APPDIR/usr/bin/tq, where APPDIR is set by the AppImage
//     runtime (the executable itself lives in usr/bin, so this is mostly a
//     belt-and-braces fallback for non-AppImage layouts);
//   - finally the TQ_BUNDLED_PATH env var.
//
// It returns "" when no bundled binary is found.
func bundledTQPath() string {
	name := "tq"
	if runtime.GOOS == "windows" {
		name = "tq.exe"
	}

	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), name))
	}
	if runtime.GOOS == "linux" {
		if appDir := os.Getenv("APPDIR"); appDir != "" {
			candidates = append(candidates, filepath.Join(appDir, "usr", "bin", name))
		}
	}
	if p := os.Getenv("TQ_BUNDLED_PATH"); p != "" {
		candidates = append(candidates, p)
	}

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}
