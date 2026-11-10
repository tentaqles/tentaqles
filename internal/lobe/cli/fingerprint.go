package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/tentaqles/tentaqles/internal/lobe/fingerprint"
	"github.com/tentaqles/tentaqles/internal/lobe/scrub"
)

// fingerprintInputMax bounds what the verb reads from stdin: 1 MiB. Anything after it is not read.
const fingerprintInputMax = 1 << 20

// Fingerprint prints the fingerprint of an error text read from in, for the tool named tool: two lines,
// the fp and the template. The text is scrubbed with scrub.Text first, as the hook scrubs it, so the
// template never holds a secret. It reads no store, resolves no client and needs no login, so it runs in
// any folder and inside an agent session.
func Fingerprint(tool string, in io.Reader, out io.Writer) error {
	if tool == "" {
		return errors.New("--tool is required")
	}
	raw, err := io.ReadAll(io.LimitReader(in, fingerprintInputMax))
	if err != nil {
		return fmt.Errorf("cannot read the error text: %w", err)
	}
	scrubbed := scrub.Text(string(raw)).String()
	fp, template := fingerprint.Of(tool, scrubbed)
	_, err = fmt.Fprintf(out, "%s\n%s\n", fp, template)
	return err
}
