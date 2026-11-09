// Package tentaqles holds the files that ship with every build: the license, the notices and the
// license texts of the projects tentaqles draws on.
package tentaqles

import "embed"

// Notices holds LICENSE, NOTICE and third_party/licenses.
//
//go:embed LICENSE NOTICE third_party/licenses
var Notices embed.FS
