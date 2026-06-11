package weather

import (
	"github.com/GoMudEngine/GoMud/internal/events"
	"github.com/GoMudEngine/GoMud/internal/mudlog"
	"github.com/GoMudEngine/GoMud/internal/util"
	"github.com/GoMudEngine/GoMud/modules/weather/content"
	"github.com/GoMudEngine/GoMud/modules/weather/engine"
	"github.com/GoMudEngine/GoMud/modules/weather/seasons"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// startSim initializes the simulation once a geography graph exists: load
// content, restore-or-seed state, reconcile the world's mutators to it, and
// schedule the first tick/emote. Safe to call again (no-ops when ready); a
// later successful 'weather rebuild' can start a sim that failed at boot.
// Degrades gracefully: with no graph the module logs once and stays idle
// (spec §2.3.5 / §10 "graceful degradation").
func (m *weatherModule) startSim(round uint64) {
	if m.simReady {
		return
	}
	if m.graph == nil {
		mudlog.Warn("Weather: no geography graph; simulation idle (fix the world and run 'weather rebuild')")
		m.publishSnapshot() // publish "idle" state so the admin page can show it
		return
	}
	m.simCfg = m.cfg.simConfig()
	m.loadContent()
	m.loadSeasons()
	if !m.cfg.BuffsEnabled {
		n := engine.StripBuffs()
		mudlog.Info("Weather: buffs disabled by config", "specsStripped", n)
	}
	m.loadOrInitState(round)
	m.applyWeather()
	m.nextTick = engine.NextTickRound(engine.TickPeriod(m.cfg.TickEveryGameHours))
	m.scheduleEmote(round)
	m.simReady = true
	m.publishSnapshot()
}

// applyWeather is the single switch between zone-scoped and room-scoped
// weather application (spec §2.1) — every path that asserts weather mutators
// funnels through here. Seasons are always zone-wide and untouched here.
// Game loop only.
func (m *weatherModule) applyWeather() {
	if m.cfg.PerRoomRefinement == RefineOff {
		engine.Reconcile(m.state.Weather)
		return
	}
	// Room-scoped modes own the weather footprint: clear the zone-level
	// mutators first so rooms are the only carriers.
	engine.StripZoneWeather(m.graph)
	switch m.cfg.PerRoomRefinement {
	case RefineAll:
		// Refining every room force-loads unloaded rooms by design — the
		// documented cost of "all"; "occupied" never force-loads.
		for _, zone := range m.graph.Zones() {
			for _, roomId := range engine.ZoneRoomIds(zone) {
				engine.RefineRoom(roomId, m.state.Weather)
			}
		}
	default: // RefineOccupied
		engine.RefineOccupiedRooms(m.state.Weather)
	}
}

// onRoomChange keeps "occupied" mode current between ticks: refine the room a
// player enters, strip the one they left once it empties. Runs on the game
// loop; the event is queued after the engine completes the move, so room
// player counts are post-move here. Modes "all"/"off" need no entry hook
// (every room is already covered / weather is zone-scoped).
func (m *weatherModule) onRoomChange(e events.Event) events.ListenerReturn {
	evt, ok := e.(events.RoomChange)
	if !ok || evt.UserId == 0 { // mobs don't need refinement on the move
		return events.Continue
	}
	if !m.simReady || m.cfg.PerRoomRefinement != RefineOccupied {
		return events.Continue
	}
	// Always refine the destination — logins fire RoomChange with From==To
	// (world.go enterWorld → MoveToRoom into the saved room), and that room
	// just became occupied. RefineRoom is idempotent, so this is cheap.
	engine.RefineRoom(evt.ToRoomId, m.state.Weather)
	if evt.FromRoomId > 0 && evt.FromRoomId != evt.ToRoomId && !engine.RoomHasPlayers(evt.FromRoomId) {
		engine.StripRoomWeather(evt.FromRoomId)
	}
	return events.Continue
}

// loadContent loads climate overrides and emote tables from the module's
// embedded files. Both fail soft: defaults / silence plus a warning.
func (m *weatherModule) loadContent() {
	climate, err := content.LoadClimate(files, "files/datafiles/climate")
	if err != nil {
		mudlog.Warn("Weather: climate overrides failed to load; using defaults", "error", err)
	}
	m.climate = climate

	tables, err := content.LoadEmotes(files, "files/datafiles/emotes")
	if err != nil {
		mudlog.Warn("Weather: emote tables failed to load", "error", err)
	}
	m.tables = tables

	seasonalTables, err := content.LoadSeasonalEmotes(files, "files/datafiles/emotes/seasons")
	if err != nil {
		mudlog.Warn("Weather: seasonal emote tables failed to load", "error", err)
	}
	m.seasonalTables = seasonalTables
}

// loadSeasons loads season tracks and establishes the baseline per-zone
// season map. Fail-soft ladder (design spec §7): disabled by config, no
// usable calendar, no/invalid track files => m.seasonsOn stays false and
// weather runs exactly as v1.
func (m *weatherModule) loadSeasons() {
	m.seasonsOn = false
	if !m.cfg.SeasonsEnabled {
		return
	}
	months, days := engine.CalendarShape()
	if months < 1 || days < 1 {
		mudlog.Warn("Weather: no usable calendar; seasons disabled")
		return
	}
	tracks, errs := seasons.Load(files, "files/datafiles/seasons", months, days)
	for _, err := range errs {
		mudlog.Warn("Weather: season track rejected", "error", err)
	}
	if len(tracks) == 0 {
		mudlog.Warn("Weather: no season tracks loaded; seasons disabled")
		return
	}
	m.tracks = tracks
	m.seasonsOn = true
	// Baseline resolution: establishes zoneSeasons WITHOUT emitting events,
	// so reboots never replay a flood of season changes.
	m.zoneSeasons = seasons.ZoneSeasons(m.graph, m.climate, m.tracks, engine.CalendarNow())
	engine.ReconcileSeasons(m.graph, m.zoneSeasons) // assert season mutators at boot
	mudlog.Info("Weather: seasons active", "tracks", len(tracks),
		"seasonalZones", len(m.zoneSeasons))
}

// loadOrInitState restores persisted simulation state, or seeds a fresh run
// (configured Seed, else derived stably from the world's zone names).
func (m *weatherModule) loadOrInitState(round uint64) {
	if m.cfg.Persist {
		if b, err := m.plug.ReadBytes(engine.StateIdentifier); err == nil {
			if s, ok := engine.DecodeState(b); ok {
				m.state = s
				mudlog.Info("Weather: restored simulation state",
					"fronts", len(s.Fronts), "savedRound", s.Round)
				return
			}
		}
	}
	seed := m.cfg.Seed
	if seed == 0 {
		seed = sim.DeriveSeed(m.graph)
	}
	m.state = sim.NewState(seed)
	mudlog.Info("Weather: fresh simulation state", "seed", seed, "currentRound", round)
}

// tick advances the simulation one coarse step. Reconcile (rather than a bare
// diff-apply) re-asserts any mutator the specs' decayrate safety net dropped
// between ticks, so engine-side decay drift self-corrects within one tick.
func (m *weatherModule) tick(round uint64) {
	climate := m.climate
	if m.seasonsOn {
		climate = seasons.EffectiveClimate(m.climate, m.tracks, engine.CalendarNow())
	}
	next, diff := sim.Step(m.state, m.graph, climate, m.simCfg, sim.Clock{Round: round})
	m.state = next
	_ = diff // per-zone changes are implied by the reconcile below
	m.applyWeather()
	if m.seasonsOn {
		m.resolveSeasons()
	}
	m.persistState()
	m.nextTick = engine.NextTickRound(engine.TickPeriod(m.cfg.TickEveryGameHours))
	m.publishSnapshot()
}

// resolveSeasons re-resolves every zone's season and queues a
// WeatherSeasonChanged event for each flip since the previous tick. Cross-
// track changes (a zone's biome reassigned by an admin rebuild) are not
// calendar flips and emit nothing — listeners may assume From/To are seasons
// of the same track.
func (m *weatherModule) resolveSeasons() {
	zs := seasons.ZoneSeasons(m.graph, m.climate, m.tracks, engine.CalendarNow())
	for zone, cur := range zs {
		if prev, ok := m.zoneSeasons[zone]; ok && prev.Track == cur.Track && prev.Season != cur.Season {
			events.AddToQueue(WeatherSeasonChanged{
				Zone: zone, Track: cur.Track, From: prev.Season, To: cur.Season,
			})
		}
	}
	m.zoneSeasons = zs
	engine.ReconcileSeasons(m.graph, zs)
}

// persistState writes the current state to plugin storage (cheap: a few KB
// once per game hour). Also invoked from the engine's save callback.
func (m *weatherModule) persistState() {
	if !m.cfg.Persist {
		return
	}
	b, err := engine.EncodeState(m.state)
	if err != nil {
		mudlog.Error("Weather: state encode failed", "error", err)
		return
	}
	if err := m.plug.WriteBytes(engine.StateIdentifier, b); err != nil {
		mudlog.Error("Weather: state save failed", "error", err)
	}
}

// onSave is the plugins.Save() hook (autosave, shutdown, copyover).
func (m *weatherModule) onSave() {
	if m.simReady {
		m.persistState()
	}
}

// scheduleEmote picks the next ambient-emote round: the configured cadence
// jittered by ±25% so ambiance doesn't metronome.
func (m *weatherModule) scheduleEmote(round uint64) {
	every := m.cfg.EmoteEveryRounds
	delta := every
	if jitter := every / 4; jitter > 0 {
		delta += util.Rand(2*jitter+1) - jitter
	}
	if delta < 1 {
		delta = 1
	}
	m.nextEmote = round + uint64(delta)
}
