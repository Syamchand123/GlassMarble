package errors

import "errors"

var (
	ErrNoAKGDatabase     = errors.New("AKG not found -- run: glassmarble analyze")
	ErrUnsupportedLang   = errors.New("language not supported")
	ErrParseFailure      = errors.New("file parsing failed")
	ErrLockTimeout       = errors.New("database lock timeout")
	ErrSchemaVersion     = errors.New("incompatible AKG schema version -- re-run analyze")
	ErrInvalidEntryPoint = errors.New("entry point not found in graph")
	ErrEmptyGraph        = errors.New("AKG has no nodes -- run analyze first")
)
