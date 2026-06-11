package engine

import (
	"reflect"
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// fakeMutatorSet records operations. The live state lives in the `current`
// slice the caller passes to reconcileList, so the fake needs no state of its
// own.
type fakeMutatorSet struct {
	ops []string
}

func newFake() *fakeMutatorSet { return &fakeMutatorSet{} }
func (f *fakeMutatorSet) Add(id string) bool {
	f.ops = append(f.ops, "add:"+id)
	return true
}
func (f *fakeMutatorSet) Remove(id string) bool {
	f.ops = append(f.ops, "remove:"+id)
	return true
}

func TestMutatorIdFor(t *testing.T) {
	if got := MutatorIdFor("storm"); got != "weather-storm" {
		t.Errorf("storm -> %q", got)
	}
	if MutatorIdFor(sim.Clear) != "" || MutatorIdFor("") != "" {
		t.Error("clear and unset must map to no mutator")
	}
}

func TestSeasonMutatorId(t *testing.T) {
	if got := SeasonMutatorId("temperate", "winter"); got != "season-temperate-winter" {
		t.Errorf("got %q", got)
	}
	if SeasonMutatorId("", "winter") != "" || SeasonMutatorId("temperate", "") != "" {
		t.Error("empty track or season must map to no mutator")
	}
}

func TestReconcileSeasonZone(t *testing.T) {
	// Same core as weather: stale season swapped for the current one,
	// weather-* ids untouched because the caller gathers season-* only.
	f := newFake()
	reconcileList(f, []string{"season-temperate-autumn"}, SeasonMutatorId("temperate", "winter"))
	want := []string{"remove:season-temperate-autumn", "add:season-temperate-winter"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}
}

func TestReconcileList(t *testing.T) {
	// Stale storm + stray fog live; target is rain.
	f := newFake()
	reconcileList(f, []string{"weather-storm", "weather-fog"}, MutatorIdFor("rain"))
	want := []string{"remove:weather-storm", "remove:weather-fog", "add:weather-rain"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}

	// Target already live: only the stray is removed.
	f = newFake()
	reconcileList(f, []string{"weather-rain", "weather-fog"}, MutatorIdFor("rain"))
	want = []string{"remove:weather-fog"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}

	// Calm target: everything weather-* goes.
	f = newFake()
	reconcileList(f, []string{"weather-snow"}, MutatorIdFor(sim.Clear))
	want = []string{"remove:weather-snow"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}
}
