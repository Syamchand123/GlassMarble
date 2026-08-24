package product

import (
	"os"
	"strings"
)

// Feature flag helpers for material behavior switches (§14.0).

// IsSchemaV3Enabled returns true unless GMB_SCHEMA_V3 is explicitly set to "0" or "false" (case-insensitive, C6-D22).
func IsSchemaV3Enabled() bool {
	v := os.Getenv("GMB_SCHEMA_V3")
	return !strings.EqualFold(v, "0") && !strings.EqualFold(v, "false")
}

// IsNewAggregatorEnabled returns true unless GMB_NEW_AGGREGATOR is explicitly set to "0" or "false" (C6-D22).
func IsNewAggregatorEnabled() bool {
	v := os.Getenv("GMB_NEW_AGGREGATOR")
	return !strings.EqualFold(v, "0") && !strings.EqualFold(v, "false")
}

// IsNewLinkerEnabled returns true unless GMB_NEW_LINKER is explicitly set to "0" or "false" (C6-D22).
func IsNewLinkerEnabled() bool {
	v := os.Getenv("GMB_NEW_LINKER")
	return !strings.EqualFold(v, "0") && !strings.EqualFold(v, "false")
}
