package invalidator

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/doc_engine/config"
	"github.com/stretchr/testify/assert"
)

func TestHashSection_Determinism(t *testing.T) {
	sec := &config.SectionSpec{
		ID:          "interface",
		Title:       "Exported Interface",
		Instruction: "Table of types and methods",
		GroundWith:  []string{"signatures", "exported_symbols"},
	}

	symbols := []config.SymbolFact{
		{FQN: "internal/auth.ValidateToken", Kind: "func", Signature: "func ValidateToken() error", Doc: "Validates JWT"},
		{FQN: "internal/auth.ErrExpired", Kind: "var", Signature: "var ErrExpired error", Doc: "Expired"},
	}

	diagrams := []config.DiagramRef{
		{Type: "callgraph", Entry: "internal/auth.ValidateToken"},
	}

	// 1. Same input produces identical hash
	hash1 := HashSection(sec, symbols, diagrams)
	hash2 := HashSection(sec, symbols, diagrams)
	assert.Equal(t, hash1, hash2)
	assert.Len(t, hash1, 64) // SHA256 hex string

	// 2. Changing symbol order does not change hash (deterministic sorting)
	reversedSymbols := []config.SymbolFact{symbols[1], symbols[0]}
	hashReversed := HashSection(sec, reversedSymbols, diagrams)
	assert.Equal(t, hash1, hashReversed)

	// 3. Changing symbol signature alters hash
	modifiedSymbols := []config.SymbolFact{
		{FQN: "internal/auth.ValidateToken", Kind: "func", Signature: "func ValidateToken(ctx context.Context) error", Doc: "Validates JWT"},
		symbols[1],
	}
	hashModified := HashSection(sec, modifiedSymbols, diagrams)
	assert.NotEqual(t, hash1, hashModified)

	// 4. Changing instruction alters hash
	modifiedSec := *sec
	modifiedSec.Instruction = "Different instruction"
	hashNewInstruction := HashSection(&modifiedSec, symbols, diagrams)
	assert.NotEqual(t, hash1, hashNewInstruction)
}

func TestNormalizeSignature(t *testing.T) {
	sig1 := "func   Foo( a int ,   b string ) error"
	sig2 := "func Foo( a int , b string ) error"
	assert.Equal(t, NormalizeSignature(sig1), NormalizeSignature(sig2))
}
