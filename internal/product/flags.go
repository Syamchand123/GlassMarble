package product

import "os"

// Feature flag helpers for material behavior switches (§14.0).

// IsSchemaV3Enabled returns true unless GMB_SCHEMA_V3 is explicitly set to "0" or "false".
func IsSchemaV3Enabled() bool {
	v := os.Getenv("GMB_SCHEMA_V3")
	return v != "0" && v != "false"
}

// IsNewStage3Enabled returns true unless GMB_NEW_STAGE3 is explicitly set to "0" or "false".
func IsNewStage3Enabled() bool {
	v := os.Getenv("GMB_NEW_STAGE3")
	return v != "0" && v != "false"
}

// IsNewStage4Enabled returns true unless GMB_NEW_STAGE4 is explicitly set to "0" or "false".
func IsNewStage4Enabled() bool {
	v := os.Getenv("GMB_NEW_STAGE4")
	return v != "0" && v != "false"
}
