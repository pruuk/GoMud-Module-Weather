package weather

// WeatherSeasonChanged is queued on the engine event bus when a zone's
// resolved season flips (never on the baseline-establishing first resolution
// after boot). Other modules listen by importing this type:
//
//	events.RegisterListener(weather.WeatherSeasonChanged{}, handler)
type WeatherSeasonChanged struct {
	Zone  string
	Track string
	From  string
	To    string
}

// Type implements events.Event.
func (WeatherSeasonChanged) Type() string { return `WeatherSeasonChanged` }

// WeatherAdminAction is queued by the admin web API; executed on the game
// loop through the same paths as the in-game admin commands.
type WeatherAdminAction struct {
	Action    string  // "spawn" | "clear" | "rebuild"
	Weather   string  // spawn: weather type
	Zone      string  // spawn: required; clear: optional
	Intensity float64 // spawn: 0 => default
}

// Type implements events.Event.
func (WeatherAdminAction) Type() string { return `WeatherAdminAction` }

// WeatherConfigChanged is queued after the admin web API persists a config
// write; the module re-reads config on the game loop and applies live keys.
type WeatherConfigChanged struct {
	Key string
}

// Type implements events.Event.
func (WeatherConfigChanged) Type() string { return `WeatherConfigChanged` }
