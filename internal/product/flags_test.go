package product

import (
	"os"
	"testing"
)

func TestFeatureFlags(t *testing.T) {
	t.Run("default enabled", func(t *testing.T) {
		os.Unsetenv("GMB_SCHEMA_V3")
		os.Unsetenv("GMB_NEW_AGGREGATOR")
		os.Unsetenv("GMB_NEW_LINKER")

		if !IsSchemaV3Enabled() || !IsNewAggregatorEnabled() || !IsNewLinkerEnabled() {
			t.Fatalf("expected all feature flags enabled by default")
		}
	})

	t.Run("explicit disable", func(t *testing.T) {
		os.Setenv("GMB_SCHEMA_V3", "0")
		os.Setenv("GMB_NEW_AGGREGATOR", "false")
		os.Setenv("GMB_NEW_LINKER", "0")

		if IsSchemaV3Enabled() || IsNewAggregatorEnabled() || IsNewLinkerEnabled() {
			t.Fatalf("expected all feature flags to be disabled")
		}

		os.Unsetenv("GMB_SCHEMA_V3")
		os.Unsetenv("GMB_NEW_AGGREGATOR")
		os.Unsetenv("GMB_NEW_LINKER")
	})
}
