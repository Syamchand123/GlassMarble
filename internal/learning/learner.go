package learning

import (
	"errors"
	"fmt"

	"github.com/Syamchand123/GlassMarble/internal/archmodel"
	"github.com/Syamchand123/GlassMarble/internal/config"
	"github.com/Syamchand123/GlassMarble/internal/developer_memory"
)

// ErrNotUndoable is returned by Undo when a correction kind cannot be
// reverted (REJECT/ACCEPT are flags, not value changes — reversing them
// would require a new correction of the opposite kind, which is the user's
// call, not an automatic action).
var ErrNotUndoable = errors.New("learning: correction kind cannot be auto-reverted (append a compensating correction of the opposite kind)")

// ErrNotFound is returned when an undo or lookup references a correction
// that is not in the log.
var ErrNotFound = errors.New("learning: correction not found in log")

// Learner is the convention learning facade. It owns the append-only correction log
// and knows how to overlay it onto memory query results and aggregates —
// the single entry point the CLI and (later) evidence retrieval evidence retrieval
// use. It NEVER writes to the source-of-truth memory WALs.
type Learner struct {
	store *Store
	cfg   *config.LearningConfig
}

// Option configures a Learner.
type Option func(*Learner)

// WithConfig overrides the learner's configuration (defaults when nil).
func WithConfig(cfg *config.LearningConfig) Option {
	return func(l *Learner) { l.cfg = cfg }
}

// NewLearner creates a Learner over the given correction store.
func NewLearner(store *Store, opts ...Option) *Learner {
	l := &Learner{store: store, cfg: config.DefaultLearningConfig()}
	for _, o := range opts {
		o(l)
	}
	if l.cfg == nil {
		l.cfg = config.DefaultLearningConfig()
	}
	return l
}

// NewLearnerForRepo builds a Learner for a repository root, wiring the
// standard correction-log path (master plan §8.2).
func NewLearnerForRepo(repoDir string, opts ...Option) *Learner {
	return NewLearner(NewStore(repoDir), opts...)
}

// Store returns the underlying correction store (for callers that need the
// raw log).
func (l *Learner) Store() *Store {
	return l.store
}

// Config returns the effective learning configuration.
func (l *Learner) Config() *config.LearningConfig {
	return l.cfg
}

// Correct records one correction. The timestamp and deterministic ID are
// filled in by the store; the correction is validated before anything is
// written, and the append is fsynced. Returns the persisted correction.
//
// When the correction's original value is empty and the target can be
// resolved in memory, the currently displayed value is captured
// automatically so the audit trail is complete.
func (l *Learner) Correct(c Correction, mem *developer_memory.DeveloperMemory) (Correction, error) {
	if c.OriginalValue == "" && mem != nil {
		if before, ok := displayValue(c, mem); ok {
			c.OriginalValue = before
		}
	}
	return l.store.Append(c)
}

// List returns the full audit log, oldest first.
func (l *Learner) List() ([]Correction, error) {
	return l.store.LoadAll()
}

// OverlayQuery projects a ranked memory query result with the correction
// log applied. When apply_on_query is disabled in the learning config, the
// result is returned untouched (with no audit entries).
func (l *Learner) OverlayQuery(res *developer_memory.MemoryQueryResult) (*CorrectedResult, error) {
	if !l.cfg.CorrectionsApplyOnQuery() {
		return &CorrectedResult{MemoryQueryResult: res}, nil
	}
	corrections, err := l.store.LoadAll()
	if err != nil {
		return nil, err
	}
	return Apply(res, corrections), nil
}

// Query runs a deterministic memory query and overlays the corrections —
// the corrected query path the CLI uses (master plan §8.3: corrections are
// reflected on every memory query).
func (l *Learner) Query(store *developer_memory.MemoryStore, ask string) (*CorrectedResult, error) {
	return l.OverlayQuery(developer_memory.QueryMemory(store, ask))
}

// OverlayMemory projects the full memory aggregate with corrections applied
// (overview and --component paths). Returns the projection and its audit
// trail.
func (l *Learner) OverlayMemory(mem *developer_memory.DeveloperMemory) (*developer_memory.DeveloperMemory, []AppliedCorrection, error) {
	if !l.cfg.CorrectionsApplyOnQuery() {
		return mem, nil, nil
	}
	corrections, err := l.store.LoadAll()
	if err != nil {
		return nil, nil, err
	}
	proj, applied := ApplyToMemory(mem, corrections)
	return proj, applied, nil
}

// Undo reverts a previously recorded value-changing correction by
// appending a compensating correction (same target, opposite change back
// to the original value). The log itself is never rewritten — reversibility
// is achieved by append, exactly as the master plan requires.
//
// REJECT/ACCEPT cannot be auto-reverted (ErrNotUndoable): they are flags,
// and the correct way to reverse them is a correction of the opposite kind.
func (l *Learner) Undo(correctionID string) (Correction, error) {
	all, err := l.store.LoadAll()
	if err != nil {
		return Correction{}, err
	}
	var target *Correction
	for i := range all {
		if all[i].ID == correctionID {
			target = &all[i]
			break
		}
	}
	if target == nil {
		return Correction{}, ErrNotFound
	}
	switch target.Kind {
	case CorrectionKindReject, CorrectionKindAccept:
		return Correction{}, ErrNotUndoable
	case CorrectionKindState, CorrectionKindLabel, CorrectionKindIntent, CorrectionKindConfidence:
		// Compensating correction: restore the original value. If the
		// original was never captured, there is nothing safe to restore.
		if target.OriginalValue == "" {
			return Correction{}, fmt.Errorf("learning: correction %q has no original_value to restore", correctionID)
		}
		return l.store.Append(Correction{
			Kind:           target.Kind,
			TargetID:       target.TargetID,
			OriginalValue:  target.CorrectedValue,
			CorrectedValue: target.OriginalValue,
			Reason:         "undo of " + correctionID,
			Author:         target.Author,
		})
	}
	return Correction{}, ErrNotUndoable
}

// PatternFeedback derives the preferred and rejected architecture patterns
// from the correction log, resolving pattern events (kind PATTERN_DETECTED,
// pattern name in Components[0]) in the memory aggregate. This is what
// feeds ProjectConventions.PreferredPatterns / RejectedPatterns — learned
// from history, not guessed.
func (l *Learner) PatternFeedback(mem *developer_memory.DeveloperMemory) (preferred, rejected []string, err error) {
	if mem == nil {
		return nil, nil, nil
	}
	corrections, err := l.store.LoadAll()
	if err != nil {
		return nil, nil, err
	}
	eventByID := make(map[string]archmodel.ArchEvent, len(mem.Events))
	for _, ev := range mem.Events {
		eventByID[ev.ID] = ev
	}
	prefSeen := make(map[string]bool)
	rejSeen := make(map[string]bool)
	for _, c := range corrections {
		ev, ok := eventByID[c.TargetID]
		if !ok || ev.Kind != archmodel.EventPatternDetected || len(ev.Components) == 0 {
			continue
		}
		pattern := ev.Components[0]
		switch c.Kind {
		case CorrectionKindAccept:
			if !prefSeen[pattern] {
				prefSeen[pattern] = true
				preferred = append(preferred, pattern)
			}
		case CorrectionKindReject:
			if !rejSeen[pattern] {
				rejSeen[pattern] = true
				rejected = append(rejected, pattern)
			}
		}
	}
	return preferred, rejected, nil
}

// displayValue returns the value a correction would change, as currently
// displayed in the memory aggregate. Used to auto-fill OriginalValue.
func displayValue(c Correction, mem *developer_memory.DeveloperMemory) (string, bool) {
	for _, ev := range mem.Events {
		if ev.ID != c.TargetID {
			continue
		}
		switch c.Kind {
		case CorrectionKindIntent:
			return ev.Intent, true
		case CorrectionKindLabel:
			return ev.Title, true
		case CorrectionKindConfidence:
			return fmt.Sprintf("%.3f", ev.Evidence.AggConfidence), true
		}
		return "", false
	}
	for _, claim := range mem.GlobalMemory {
		if claim.ID != c.TargetID {
			continue
		}
		switch c.Kind {
		case CorrectionKindLabel:
			return claim.Subject, true
		case CorrectionKindState:
			return string(claim.State), true
		case CorrectionKindConfidence:
			return fmt.Sprintf("%.3f", claim.Evidence.AggConfidence), true
		}
		return "", false
	}
	if h, ok := mem.ComponentMemory[c.TargetID]; ok {
		switch c.Kind {
		case CorrectionKindLabel:
			return h.Name, true
		case CorrectionKindState:
			return string(h.State), true
		}
	}
	return "", false
}
