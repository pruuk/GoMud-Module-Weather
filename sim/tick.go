package sim

import "sort"

// Step advances the simulation one coarse tick. It is a pure function of its
// inputs: all randomness comes from prev.RNGState. It returns the next State
// (fronts advanced, weather resolved, RNG cursor written back) and the StateDiff
// of per-zone weather changes the engine layer should apply.
func Step(prev State, g *Graph, climate Climate, cfg Config, now Clock) (State, StateDiff) {
	rng := NewRNG(prev.RNGState)

	fronts := cloneFronts(prev.Fronts)
	fronts = ageAndFeedback(fronts, g, climate)
	fronts = moveFronts(fronts, g, climate, cfg, rng)
	fronts = evolveTypes(fronts, g, climate, rng)
	fronts = removeDead(fronts, cfg)

	nextID := prev.NextID
	fronts, nextID = spawnFronts(fronts, g, climate, cfg, rng, nextID)

	weather := resolveWeather(g, fronts, cfg)
	diff := diffWeather(prev.Weather, weather)

	return State{
		Round:    now.Round,
		RNGState: rng.State(),
		NextID:   nextID,
		Fronts:   fronts,
		Weather:  weather,
	}, diff
}

func cloneFronts(in []Front) []Front {
	out := make([]Front, len(in))
	copy(out, in)
	for i := range out {
		out[i].History = append([]ZoneId(nil), in[i].History...)
	}
	return out
}

// ageAndFeedback ages each front and applies the influence of the biome of the
// zone it currently occupies (the weather <- biome half of the feedback loop),
// plus an age-based decay once past MaxAge. Intensity/Moisture are clamped.
//
// Mutates the elements of fronts in place; callers must pass a copy they own
// (Step does, via cloneFronts).
func ageAndFeedback(fronts []Front, g *Graph, climate Climate) []Front {
	for i := range fronts {
		f := &fronts[i]
		f.Age++
		inf := climate.For(g.Nodes[f.Zone].Biome).Influence
		f.Intensity += inf.IntensityDelta
		f.Moisture += inf.MoistureDelta
		if f.MaxAge > 0 && f.Age > f.MaxAge {
			f.Intensity -= 0.05 * float64(f.Age-f.MaxAge)
		}
		f.Intensity = clamp01(f.Intensity)
		f.Moisture = clamp01(f.Moisture)
	}
	return fronts
}

// removeDead drops fronts whose intensity has reached zero or that exceed the
// hard age cap. It filters in place (reusing the backing array), so callers must
// pass a copy they own (Step does, via cloneFronts).
func removeDead(fronts []Front, cfg Config) []Front {
	out := fronts[:0]
	for _, f := range fronts {
		if f.Intensity <= 0 {
			continue
		}
		if cfg.FrontHardAge > 0 && f.Age > cfg.FrontHardAge {
			continue
		}
		out = append(out, f)
	}
	return out
}

// resolveWeather computes per-zone weather with intensity-scaled area coverage:
// each front projects onto zones within cfg.MaxFrontRadius hops of its center,
// with projected intensity = Intensity * CoverageFalloff^hops; a zone is covered
// only while that stays >= cfg.MinProjected. Per zone, the front projecting the
// highest effective intensity wins (deterministic tie-break by lowest FrontId).
// Zones no front reaches are Clear. Stronger fronts naturally cover more zones.
func resolveWeather(g *Graph, fronts []Front, cfg Config) map[ZoneId]WeatherType {
	type claim struct {
		eff   float64
		id    FrontId
		wtype WeatherType
	}
	best := map[ZoneId]claim{}
	for i := range fronts {
		f := &fronts[i]
		for zone, hops := range zonesWithin(g, f.Zone, cfg.MaxFrontRadius) {
			eff := f.Intensity * pow(cfg.CoverageFalloff, hops)
			if eff < cfg.MinProjected {
				continue
			}
			cur, ok := best[zone]
			if !ok || eff > cur.eff || (eff == cur.eff && f.Id < cur.id) {
				best[zone] = claim{eff: eff, id: f.Id, wtype: f.Type}
			}
		}
	}
	out := make(map[ZoneId]WeatherType, len(g.Nodes))
	for _, z := range g.Zones() {
		if c, ok := best[z]; ok {
			out[z] = c.wtype
		} else {
			out[z] = Clear
		}
	}
	return out
}

// zonesWithin returns each zone reachable from center within maxRadius hops,
// mapped to its hop-distance (center is 0). Deterministic shortest-path BFS;
// its result is order-independent, so resolveWeather stays deterministic.
func zonesWithin(g *Graph, center ZoneId, maxRadius int) map[ZoneId]int {
	dist := map[ZoneId]int{center: 0}
	frontier := []ZoneId{center}
	for d := 0; d < maxRadius; d++ {
		var next []ZoneId
		for _, z := range frontier {
			for _, e := range g.Neighbors(z) {
				if _, seen := dist[e.B]; !seen {
					dist[e.B] = d + 1
					next = append(next, e.B)
				}
			}
		}
		frontier = next
	}
	return dist
}

// pow raises base to a non-negative integer power (small exponents; avoids
// importing math).
func pow(base float64, exp int) float64 {
	r := 1.0
	for i := 0; i < exp; i++ {
		r *= base
	}
	return r
}

// diffWeather returns the zones whose weather changed from prev to next, sorted
// by zone for deterministic output.
func diffWeather(prev, next map[ZoneId]WeatherType) StateDiff {
	zones := make([]ZoneId, 0, len(next))
	for z := range next {
		zones = append(zones, z)
	}
	sort.Strings(zones)

	var changes []ZoneChange
	for _, z := range zones {
		from := prev[z] // "" if previously unknown
		if next[z] != from {
			changes = append(changes, ZoneChange{Zone: z, From: from, To: next[z]})
		}
	}
	return StateDiff{Changes: changes}
}

// --- stubs replaced in later tasks ---

func moveFronts(fronts []Front, g *Graph, climate Climate, cfg Config, rng *RNG) []Front {
	return fronts
}

func evolveTypes(fronts []Front, g *Graph, climate Climate, rng *RNG) []Front {
	return fronts
}

func spawnFronts(fronts []Front, g *Graph, climate Climate, cfg Config, rng *RNG, nextID FrontId) ([]Front, FrontId) {
	return fronts, nextID
}
