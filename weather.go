package weather

import (
	"embed"

	"github.com/GoMudEngine/GoMud/internal/events"
	"github.com/GoMudEngine/GoMud/internal/mudlog"
	"github.com/GoMudEngine/GoMud/internal/plugins"
	"github.com/GoMudEngine/GoMud/internal/users"
	"github.com/GoMudEngine/GoMud/internal/util"
	"github.com/GoMudEngine/GoMud/modules/weather/content"
	"github.com/GoMudEngine/GoMud/modules/weather/crawler"
	"github.com/GoMudEngine/GoMud/modules/weather/engine"
	"github.com/GoMudEngine/GoMud/modules/weather/seasons"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

//go:embed files/*
var files embed.FS

// weatherModule holds the plugin handle, resolved config, the geography graph,
// and the live simulation (state/climate/emote tables/schedule). All fields are
// touched only from the single game-loop goroutine — no synchronization needed.
type weatherModule struct {
	plug    *plugins.Plugin
	cfg     Config
	graph   *sim.Graph
	started bool

	simReady       bool // graph + content + state loaded; ticking enabled
	simCfg         sim.Config
	climate        sim.Climate
	tables         content.Tables
	seasonalTables content.SeasonalTables // seasonal-ambience emotes (track,season)-keyed
	state          sim.State
	nextTick       uint64 // round number when the next weather tick fires
	nextEmote      uint64 // round number when the next ambient emote pass fires

	tracks      seasons.Tracks                    // loaded season tracks (nil/empty = seasons off)
	seasonsOn   bool                              // SeasonsEnabled && tracks loaded && calendar usable
	zoneSeasons map[sim.ZoneId]seasons.ZoneSeason // previous tick's resolution (event diffing)

	lastAdminAction string // most recent admin-page action result (snapshot field)
}

var module weatherModule

func init() {
	module = weatherModule{plug: plugins.New(`weather`, `0.1.0`)}
	if err := module.plug.AttachFileSystem(files); err != nil {
		panic(err)
	}
	module.plug.Callbacks.SetOnLoad(module.onLoad)
	// Command and exports are registered at init: plugins.Load() harvests the
	// command map BEFORE invoking onLoad, so anything registered there is lost.
	// Behavior (not registration) is gated on cfg.Enabled / simReady.
	module.plug.AddUserCommand(`weather`, module.cmdWeather, false, false)
	module.registerExports()
}

// onLoad loads config and registers the save hook + NewRound listener. The
// command and exports are registered in init() (plugins.Load harvests the
// command map before onLoad). World crawling and sim startup are deferred to
// the first NewRound (engine-specific onLoad timing vs world load).
func (m *weatherModule) onLoad() {
	m.cfg = loadConfig(m.plug)
	if !m.cfg.Enabled {
		return
	}
	m.plug.Callbacks.SetOnSave(m.onSave)
	events.RegisterListener(events.NewRound{}, m.onNewRound)
	events.RegisterListener(WeatherAdminAction{}, m.onAdminAction)
	events.RegisterListener(WeatherConfigChanged{}, m.onConfigChanged)
}

// onNewRound drives everything round-based: one-time startup, the jittered
// ambient-emote pass, and the coarse weather tick.
func (m *weatherModule) onNewRound(e events.Event) events.ListenerReturn {
	evt, ok := e.(events.NewRound)
	if !ok {
		return events.Continue
	}
	if !m.started {
		m.started = true
		m.loadOrBuildGraph()
		m.startSim(evt.RoundNumber)
	}
	if !m.simReady {
		return events.Continue
	}
	if m.cfg.EmoteMode == EmoteModeModule && evt.RoundNumber >= m.nextEmote {
		engine.EmitAmbient(m.state.Weather, m.zoneSeasons, m.tables, m.seasonalTables, util.Rand)
		m.scheduleEmote(evt.RoundNumber)
	}
	if evt.RoundNumber >= m.nextTick {
		m.tick(evt.RoundNumber)
	}
	return events.Continue
}

// loadOrBuildGraph uses the cached graph when present and current, otherwise
// crawls the world and persists the result.
func (m *weatherModule) loadOrBuildGraph() {
	if !m.cfg.RebuildGraphOnBoot {
		if b, err := m.plug.ReadBytes(engine.CacheIdentifier); err == nil {
			if g, ok := engine.DecodeCache(b); ok {
				m.graph = g
				mudlog.Info("Weather: loaded geography cache",
					"zones", len(g.Nodes), "edges", len(g.Edges))
				return
			}
		}
	}
	m.rebuildGraph()
}

// rebuildGraph crawls the live world, stores the graph, and writes the cache.
func (m *weatherModule) rebuildGraph() {
	opts := crawler.DefaultOptions()
	opts.IncludeSecretExits = m.cfg.IncludeSecretExits
	opts.BuiltAtRound = util.GetRoundCount()

	g, err := crawler.Build(engine.NewWorldReader(), opts)
	if err != nil {
		mudlog.Error("Weather: graph build failed", "error", err)
		return
	}
	m.graph = g

	if b, err := g.ToJSON(); err == nil {
		if err := m.plug.WriteBytes(engine.CacheIdentifier, b); err != nil {
			mudlog.Error("Weather: graph cache write failed", "error", err)
		}
	}
	mudlog.Info("Weather: built geography graph",
		"zones", len(g.Nodes), "edges", len(g.Edges), "components", g.Components)
	m.startSim(util.GetRoundCount())
	if m.simReady {
		engine.Reconcile(m.state.Weather)
		if m.seasonsOn {
			m.zoneSeasons = seasons.ZoneSeasons(m.graph, m.climate, m.tracks, engine.CalendarNow())
			engine.ReconcileSeasons(m.graph, m.zoneSeasons)
		}
	}
	m.publishSnapshot()
}

// sendLine writes one line to a user. It is the ONLY place this module calls the
// engine's SendText, isolating the one upstream-vs-DOGMud divergence: upstream
// GoMud uses SendText(text); the DOGMud fork uses SendText(category, text).
// Backporting to DOGMud is a one-line change here.
func sendLine(user *users.UserRecord, text string) {
	user.SendText(text)
}
