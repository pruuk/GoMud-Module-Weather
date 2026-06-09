package weather

import "github.com/GoMudEngine/GoMud/internal/plugins"

// Config is the resolved module configuration (keys live under
// Modules.weather.* and default from files/data-overlays/config.yaml).
type Config struct {
	Enabled            bool
	IncludeSecretExits bool
	RebuildGraphOnBoot bool
}

// getter abstracts plugin.Config.Get for testability.
type getter func(string) any

func asBool(v any) bool { b, _ := v.(bool); return b }

// buildConfig resolves config from a getter.
func buildConfig(get getter) Config {
	return Config{
		Enabled:            asBool(get("Enabled")),
		IncludeSecretExits: asBool(get("IncludeSecretExits")),
		RebuildGraphOnBoot: asBool(get("RebuildGraphOnBoot")),
	}
}

// loadConfig reads the module's live config via the plugin API.
func loadConfig(p *plugins.Plugin) Config {
	return buildConfig(func(k string) any { return p.Config.Get(k) })
}
