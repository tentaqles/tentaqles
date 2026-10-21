package doctor

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

const (
	dataScanMaxFiles = 3000
	dataScanMaxBytes = 256 << 10
)

var (
	piiColumnRe  = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(email|e_mail|cpf|cnpj|rg|ssn|passport|phone|telefone|celular|birth_?date|data_nascimento|dob|address|endereco|cep|zip_?code|card_?number|iban)(?:[^a-z0-9]|$)`)
	schemaFileRe = regexp.MustCompile(`(?i)(\.sql|\.prisma|\.dbml|\.graphql|\.proto)$|(^|/)(schema\.rb|models\.py)$`)
	dataSkipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true, "dist": true, "build": true, ".next": true, "target": true}
)

// dataBlockMissing warns when a workspace has PII-looking columns in schema or
// fixture files but no data: block in its manifest (spec §11.1). It never
// reads .env files or application code.
func dataBlockMissing(ws, root string, data manifest.Data, add func(level, code, ws, msg, fix string)) {
	if data.Enabled() {
		return
	}
	var hits []string
	seen := 0
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if dataSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		seen++
		if seen > dataScanMaxFiles {
			return filepath.SkipAll
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root))
		rel = strings.TrimPrefix(rel, "/")
		inMigrations := strings.Contains("/"+rel, "/migrations/")
		if !schemaFileRe.MatchString(rel) && !inMigrations {
			return nil
		}
		if d.Type()&fs.ModeType != 0 {
			return nil
		}
		fi, err := d.Info()
		if err != nil || fi.Size() > dataScanMaxBytes {
			return nil
		}
		raw := readBounded(p, dataScanMaxBytes)
		if piiColumnRe.Match(raw) {
			hits = append(hits, rel)
		}
		return nil
	})
	if len(hits) == 0 {
		return
	}
	shown := hits
	if len(shown) > 3 {
		shown = shown[:3]
	}
	add("warn", "data-block-missing", ws,
		fmt.Sprintf("%d schema or fixture file(s) name personal-data columns (%s) and the manifest has no data: block", len(hits), strings.Join(shown, ", ")),
		"run /tentaqles:classify-data to write the data: block (classes, paths, retention, incident authority), then tq allow "+ws)
}

// readBounded reads at most max bytes of a regular file; errors yield nil.
func readBounded(path string, max int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, max))
	return b
}
