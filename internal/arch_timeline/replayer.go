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
		// Do not blame --no-graph. Snapshots also drop the embedded graph on
		// their own, via the size auto-threshold, and that is by far the more
		// common way to arrive here: the default cut-off is 15k nodes, so on
		// any repository big enough for replay to be interesting the graph is
		// omitted without the user ever asking for it. Naming only the flag
		// sends people looking for a flag they never passed.
		return nil, fmt.Errorf("snapshot %s carries no graph, so it cannot be replayed — "+
			"it was captured with --snapshot-no-graph, or the graph exceeded the snapshot size "+
			"auto-threshold (intelligence config: snapshot_auto_threshold_nodes, "+
			"snapshot_auto_threshold_mb); raise or disable those to keep graphs in future snapshots",
			snap.ID)
	}

	r := bytes.NewReader(snap.AKGJSON)
	return akg.ImportGraphJSON(r)
}
