package arch_timeline

import (
	"bytes"
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// Replay loads a historical CodePropertyGraph from an ArchSnapshot's AKGJSON.
// This allows visualization or analysis engines to run on past states.
func Replay(snap *archmodel.ArchSnapshot) (*akg.CodePropertyGraph, error) {
	if len(snap.AKGJSON) == 0 {
		return nil, fmt.Errorf("snapshot %s does not contain AKGJSON", snap.ID)
	}

	r := bytes.NewReader(snap.AKGJSON)
	return akg.ImportGraphJSON(r)
}
