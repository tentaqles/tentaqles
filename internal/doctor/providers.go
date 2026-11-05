package doctor

import (
	"errors"

	"github.com/tentaqles/tentaqles/internal/providers"
)

// providerFileFindings lists every user provider file tq did not load (ADR-0007 round 3): an
// override of a built-in id is a warning (the built-in lists stay in force), a new id an
// error (a guard: v2 workspace whose scope names it refuses every call).
func providerFileFindings(add func(level, code, ws, msg, fix string)) {
	_, errs := providers.LoadAll()
	for _, e := range errs {
		var fe *providers.FileError
		if !errors.As(e, &fe) {
			add("error", "provider-file-invalid", "", e.Error(), "fix the files under ~/.tentaqles/providers, then run tq providers list")
			continue
		}
		level := "error"
		if fe.Builtin {
			level = "warn"
		}
		add(level, "provider-file-invalid", "", fe.Error(), fe.Fix())
	}
}
