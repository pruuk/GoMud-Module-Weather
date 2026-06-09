package weather

import "testing"

func TestBuildConfig(t *testing.T) {
	vals := map[string]any{
		"Enabled":            true,
		"IncludeSecretExits": true,
		"RebuildGraphOnBoot": false,
	}
	c := buildConfig(func(k string) any { return vals[k] })

	if !c.Enabled {
		t.Error("Enabled should be true")
	}
	if !c.IncludeSecretExits {
		t.Error("IncludeSecretExits should be true")
	}
	if c.RebuildGraphOnBoot {
		t.Error("RebuildGraphOnBoot should be false")
	}
}
