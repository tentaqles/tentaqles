package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/tentaqles/tentaqles/internal/lobe/settings"
)

// Settings is `tentaqles lobe settings`: the nine settings of the client, one line each in print order
// (settings.Lines), or with asJSON one compact JSON object on one line holding the effective values.
// It returns the errors of Target and CheckLogin; the command prints them and exits 1.
func Settings(d Deps, clientFlag string, asJSON bool, out io.Writer) error {
	ws, err := Target(d, clientFlag)
	if err != nil {
		return err
	}
	if err := CheckLogin(d, ws); err != nil {
		return err
	}
	s := settings.From(ws)
	if asJSON {
		_, err := fmt.Fprintln(out, settingsJSON(s))
		return err
	}
	for _, line := range s.Lines() {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}

// settingsJSON is the object of --json: the keys in print order, each with its effective value.
// retention_days is a number; the other values are strings. Values are plain words from the lists, so
// they need no escaping.
func settingsJSON(s settings.Settings) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, x := range s {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"` + x.Key + `":`)
		if x.Key == settings.RetentionDays {
			b.WriteString(x.Value)
		} else {
			b.WriteString(`"` + x.Value + `"`)
		}
	}
	b.WriteByte('}')
	return b.String()
}
