package engine

import (
	"github.com/GoMudEngine/GoMud/internal/rooms"
	"github.com/GoMudEngine/GoMud/modules/weather/content"
	"github.com/GoMudEngine/GoMud/modules/weather/seasons"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// seasonalEmoteOneIn throttles the seasonal-ambience layer: it fires on
// roughly 1 in N emote passes, only in calm zones — strictly quieter than
// weather (spec S-R1). Promote to config only if play feel demands.
const seasonalEmoteOneIn = 3

// EmitAmbient sends at most ONE ambient line per occupied room per pass:
// the weather line when the zone has non-calm weather (season-variant aware),
// else — at reduced cadence — the zone's seasonal ambience. zoneSeasons and
// seasonal may be nil/empty when seasons are off. roll is the presentation
// RNG (pass util.Rand) — NEVER the sim RNG. Returns lines sent.
func EmitAmbient(weather map[sim.ZoneId]sim.WeatherType, zoneSeasons map[sim.ZoneId]seasons.ZoneSeason,
	tables content.Tables, seasonal content.SeasonalTables, roll func(int) int) int {
	sent := 0
	for _, roomId := range rooms.GetRoomsWithPlayers() {
		room := rooms.LoadRoom(roomId)
		if room == nil {
			continue
		}
		biomeId := ""
		if b := room.GetBiome(); b != nil {
			biomeId = b.BiomeId
		}
		indoor := !isOutdoorBiome(biomeId)
		zs, hasSeason := zoneSeasons[room.Zone]

		if w := weather[room.Zone]; w != "" && w != sim.Clear {
			season := ""
			if hasSeason {
				season = zs.Season
			}
			if line := tables.Pick(w, biomeId, indoor, season, roll); line != "" {
				room.SendText(line)
				sent++
			}
			continue // weather wins; one line per room per pass
		}
		if hasSeason && roll(seasonalEmoteOneIn) == 0 {
			if line := seasonal.Pick(zs.Track, zs.Season, biomeId, indoor, roll); line != "" {
				room.SendText(line)
				sent++
			}
		}
	}
	return sent
}
