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
