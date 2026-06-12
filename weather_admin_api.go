package weather

import (
	"encoding/json"
	"net/http"

	"github.com/GoMudEngine/GoMud/internal/events"
	"github.com/GoMudEngine/GoMud/internal/plugins"
)

// registerAdminWeb wires the admin page, API endpoints, and permission key.
// Called from init(): plugins.Load() harvests web surface BEFORE onLoad (the
// same rule as commands — see context.md).
func (m *weatherModule) registerAdminWeb() {
	m.plug.Web.AdminPage(
		"Weather", "weather", "html/admin/weather.html",
		true, "Modules", "",
		"Weather & seasons: config, status and actions",
		"", nil)
	m.plug.Web.AdminAPIEndpoint("GET", "weather/status", m.handleAdminStatus)
	m.plug.Web.AdminAPIEndpoint("POST", "weather/config", m.handleAdminConfig, "weather.write")
	m.plug.Web.AdminAPIEndpoint("POST", "weather/action", m.handleAdminAction, "weather.write")
	m.plug.Web.RegisterPermissions(plugins.ModulePermission{
		Key:         "weather.write",
		Description: "Modify weather module config and fire weather actions",
		Category:    "Modules",
	})
}

// handleAdminStatus returns the current snapshot. Runs on a web goroutine —
// reads ONLY the atomic snapshot, never module state.
func (m *weatherModule) handleAdminStatus(_ *http.Request) (int, bool, any) {
	return http.StatusOK, true, loadSnapshot()
}

// handleAdminConfig validates and persists one config write, then queues the
// change for the game loop. Web goroutine: touches only the engine config
// layer (internally locked) and the event queue (thread-safe).
func (m *weatherModule) handleAdminConfig(r *http.Request) (int, bool, any) {
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return http.StatusBadRequest, false, "malformed body"
	}
	meta, ok := configKeyMeta[body.Key]
	if !ok {
		return http.StatusBadRequest, false, "unknown config key"
	}
	if meta.ReadOnly {
		// Synthetic summary rows (BuffOverrides.*): edited in the overlay
		// file, never through this API.
		return http.StatusBadRequest, false, "read-only config key"
	}
	// Validate mirrors buildConfig's rules but rejects what the loader would
	// silently default/clamp; the normalized value is what gets persisted.
	value := body.Value
	if meta.Validate != nil {
		norm, err := meta.Validate(body.Value)
		if err != nil {
			return http.StatusBadRequest, false, body.Key + ": " + err.Error()
		}
		value = norm
	}
	got, ok := persistConfigFn(m, body.Key, value)
	if !ok {
		return http.StatusServiceUnavailable, false, "plugin not initialised"
	}
	// Read-back guard: the engine's PluginConfig.Set DISCARDS configs.SetVal's
	// error (internal/plugins/pluginconfig.go:13), so a rejected write — e.g.
	// a key the data overlay never registered ("invalid property name") —
	// looks identical to success. The only honest check is reading the value
	// back. Get returns the engine-typed value (bool/int/float64/string after
	// the yaml round-trip) while `value` is the validator's normalized string;
	// configValuesEqual compares canonical string forms for exactly that
	// reason (the validators already canonicalized bools, enums and numbers).
	if !configValuesEqual(got, value) {
		return http.StatusInternalServerError, false,
			body.Key + ": the engine rejected this write (key not registered?) — value unchanged"
	}
	events.AddToQueue(WeatherConfigChanged{Key: body.Key})
	return http.StatusOK, true, map[string]any{"key": body.Key, "badge": meta.Badge}
}

// persistConfigFn writes one validated key through the engine config layer
// and reads back what the layer now holds (false = no plugin: fabricated test
// module; live servers always have one). A seam in the module's usual style
// (mirroring rebuildGraphFn) so handler tests can fake the engine accepting
// or rejecting the write without a live plugin registry.
var persistConfigFn = func(m *weatherModule, key, value string) (any, bool) {
	if m.plug == nil {
		return nil, false
	}
	m.plug.Config.Set(key, value)
	return m.plug.Config.Get(key), true
}

// handleAdminAction shape-validates and queues an action for the game loop.
// Web goroutine: touches only the thread-safe event queue.
func (m *weatherModule) handleAdminAction(r *http.Request) (int, bool, any) {
	var a WeatherAdminAction
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		return http.StatusBadRequest, false, "malformed body"
	}
	switch a.Action {
	case "spawn":
		if a.Zone == "" || a.Weather == "" {
			return http.StatusBadRequest, false, "spawn requires weather type and zone"
		}
	case "clear", "rebuild":
		// zone optional / none
	default:
		return http.StatusBadRequest, false, "unknown action"
	}
	events.AddToQueue(a)
	return http.StatusOK, true, "accepted — result appears in the next status refresh"
}
