package arch_timeline

import (
	"bytes"
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/akg"
	"github.com/Syamchand123/GlassMarble/internal/archmodel"
)

// Replay loads a historical CodePropertyGraph from an ArchSnapshot's AKGJSON.
// This allows visualization or analysis engines to run on past states, and is
// the basis of `gmb snapshot --replay` (the caller composes the restored
// graph with the visualization engine; arch_timeline stays free of it).
func Replay(snap *archmodel.ArchSnapshot) (*akg.CodePropertyGraph, error) {
	if snap == nil {
		return nil, fmt.Errorf("arch_timeline: Replay requires a snapshot")
	}
	if len(snap.AKGJSON) == 0 {
		return nil, fmt.Errorf("snapshot %s does not contain AKGJSON (it was captured with --no-graph)", snap.ID)
	}

	r := bytes.NewReader(snap.AKGJSON)
	return akg.ImportGraphJSON(r)
}
