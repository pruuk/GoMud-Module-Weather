package weather

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/GoMudEngine/GoMud/internal/events"
	"github.com/GoMudEngine/GoMud/internal/mudlog"
	"github.com/GoMudEngine/GoMud/internal/plugins"
	"github.com/GoMudEngine/GoMud/internal/rooms"
	"github.com/GoMudEngine/GoMud/internal/users"
	"github.com/GoMudEngine/GoMud/internal/util"
	"github.com/GoMudEngine/GoMud/modules/weather/content"
	"github.com/GoMudEngine/GoMud/modules/weather/crawler"
	"github.com/GoMudEngine/GoMud/modules/weather/engine"
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

	simReady  bool   // graph + content + state loaded; ticking enabled
	simCfg    sim.Config
	climate   sim.Climate
	tables    content.Tables
	state     sim.State
	nextTick  uint64 // round number when the next weather tick fires
	nextEmote uint64 // round number when the next ambient emote pass fires
}

var module weatherModule

func init() {
	module = weatherModule{plug: plugins.New(`weather`, `0.1.0`)}
	if err := module.plug.AttachFileSystem(files); err != nil {
		panic(err)
	}
	module.plug.Callbacks.SetOnLoad(module.onLoad)
}

// onLoad loads config and registers the command, exports, and listeners. World
// crawling and sim startup are deferred to the first NewRound (engine-specific
// onLoad timing vs world load).
func (m *weatherModule) onLoad() {
	m.cfg = loadConfig(m.plug)
	if !m.cfg.Enabled {
		return
	}
	m.plug.AddUserCommand(`weather`, m.cmdWeather, false, false) // player command; admin subcommands gated in-handler
	m.registerExports()
	m.plug.Callbacks.SetOnSave(m.onSave)
	events.RegisterListener(events.NewRound{}, m.onNewRound)
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
		engine.EmitAmbient(m.state.Weather, m.tables, util.Rand)
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
}

// sendLine writes one line to a user. It is the ONLY place this module calls the
// engine's SendText, isolating the one upstream-vs-DOGMud divergence: upstream
// GoMud uses SendText(text); the DOGMud fork uses SendText(category, text).
// Backporting to DOGMud is a one-line change here.
func sendLine(user *users.UserRecord, text string) {
	user.SendText(text)
}

// cmdWeather is the admin command. Subcommands:
//
//	weather                -> graph summary
//	weather graph [zone]   -> neighbors of a zone (default: the caller's zone)
//	weather rebuild        -> re-crawl the world and rewrite the cache
func (m *weatherModule) cmdWeather(rest string, user *users.UserRecord, room *rooms.Room, flags events.EventFlag) (bool, error) {
	args := strings.Fields(rest)
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	switch sub {
	case "rebuild":
		m.rebuildGraph()
		if m.graph == nil {
			sendLine(user, "Weather: graph rebuild failed (see server log).")
			return true, nil
		}
		sendLine(user, fmt.Sprintf(
			"Weather: rebuilt graph — %d zones, %d edges, %d components.",
			len(m.graph.Nodes), len(m.graph.Edges), m.graph.Components))
	case "graph":
		zone := strings.TrimSpace(rest[len(args[0]):])
		if zone == "" && room != nil {
			zone = room.Zone
		}
		m.printGraphForZone(user, zone)
	default:
		m.printSummary(user)
	}
	return true, nil
}

func (m *weatherModule) printSummary(user *users.UserRecord) {
	if m.graph == nil {
		sendLine(user, "Weather: no geography graph yet (built on the first round). Try 'weather rebuild'.")
		return
	}
	g := m.graph
	sendLine(user, fmt.Sprintf(
		"Weather geography: %d zones, %d edges, %d components (built round %d).",
		len(g.Nodes), len(g.Edges), g.Components, g.BuiltAtRound))
}

func (m *weatherModule) printGraphForZone(user *users.UserRecord, zone string) {
	if m.graph == nil {
		sendLine(user, "Weather: no geography graph yet (built on the first round). Try 'weather rebuild'.")
		return
	}
	node, ok := m.graph.Nodes[zone]
	if !ok {
		sendLine(user, fmt.Sprintf("Weather: zone %q is not in the graph.", zone))
		return
	}
	sendLine(user, fmt.Sprintf(
		"Zone %s [biome=%s rooms=%d outdoor=%v]:", node.Zone, node.Biome, node.Rooms, node.HasOutdoor))

	neighbors := append([]sim.Edge(nil), m.graph.Neighbors(zone)...)
	if len(neighbors) == 0 {
		sendLine(user, "  (no adjacent zones)")
		return
	}
	sort.Slice(neighbors, func(i, j int) bool { return neighbors[i].B < neighbors[j].B })
	for _, e := range neighbors {
		sendLine(user, fmt.Sprintf("  -> %s (weight %d)", e.B, e.Weight))
	}
}
