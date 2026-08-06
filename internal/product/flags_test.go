package product

import (
	"os"
	"testing"
)

func TestFeatureFlags(t *testing.T) {
	t.Run("default enabled", func(t *testing.T) {
		os.Unsetenv("GMB_SCHEMA_V3")
		os.Unsetenv("GMB_NEW_STAGE3")
		os.Unsetenv("GMB_NEW_STAGE4")

		if !IsSchemaV3Enabled() || !IsNewStage3Enabled() || !IsNewStage4Enabled() {
			t.Fatalf("expected all feature flags enabled by default")
		}
	})

	t.Run("explicit disable", func(t *testing.T) {
		os.Setenv("GMB_SCHEMA_V3", "0")
		os.Setenv("GMB_NEW_STAGE3", "false")
		os.Setenv("GMB_NEW_STAGE4", "0")

		if IsSchemaV3Enabled() || IsNewStage3Enabled() || IsNewStage4Enabled() {
			t.Fatalf("expected all feature flags to be disabled")
		}

		os.Unsetenv("GMB_SCHEMA_V3")
		os.Unsetenv("GMB_NEW_STAGE3")
		os.Unsetenv("GMB_NEW_STAGE4")
	})
}
