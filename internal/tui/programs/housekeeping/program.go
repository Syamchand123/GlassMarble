// Package housekeeping provides the interactive Huh confirm dialog used by
// `gmb housekeeping --prune` before it deletes stale working-set files.
package housekeeping

import (
	"errors"
	"io"

	"github.com/charmbracelet/huh"
)

// ConfirmPrune runs an interactive Huh confirm form asking the user whether to
// proceed with the prune. desc carries the prune scope (retention window, file
// count, reclaimed bytes) built by the command layer. It returns true only when
// the user explicitly confirms; a cancelled dialog or an early exit (Ctrl+C)
// yields false, nil so the caller can abort without deleting.
func ConfirmPrune(in io.Reader, out io.Writer, desc string) (bool, error) {
	var confirmed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Prune old files?").
				Description(desc).
				Affirmative("Yes, delete").
				Negative("No, cancel").
				Value(&confirmed),
		),
	)
	form.WithInput(in)
	form.WithOutput(out)

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, nil
		}
		return false, err
	}
	return confirmed, nil
}
