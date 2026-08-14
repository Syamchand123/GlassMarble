package product

import "os"

// Feature flag helpers for material behavior switches (§14.0).

// IsSchemaV3Enabled returns true unless GMB_SCHEMA_V3 is explicitly set to "0" or "false".
func IsSchemaV3Enabled() bool {
	v := os.Getenv("GMB_SCHEMA_V3")
	return v != "0" && v != "false"
}

// IsNewAggregatorEnabled returns true unless GMB_NEW_AGGREGATOR is explicitly set to "0" or "false".
func IsNewAggregatorEnabled() bool {
	v := os.Getenv("GMB_NEW_AGGREGATOR")
	return v != "0" && v != "false"
}

// IsNewLinkerEnabled returns true unless GMB_NEW_LINKER is explicitly set to "0" or "false".
func IsNewLinkerEnabled() bool {
	v := os.Getenv("GMB_NEW_LINKER")
	return v != "0" && v != "false"
}
