package engine

import (
	"reflect"
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

func TestRoomWantId(t *testing.T) {
	cases := []struct {
		weather sim.WeatherType
		indoor  bool
		want    string
	}{
		{"storm", false, "weather-storm"},
		{"storm", true, "weather-storm-indoor"},
		{sim.Clear, false, ""},
		{sim.Clear, true, ""},
		{"", true, ""},
	}
	for _, c := range cases {
		if got := roomWantId(c.weather, c.indoor); got != c.want {
			t.Errorf("(%q,%v): got %q want %q", c.weather, c.indoor, got, c.want)
		}
	}
}

func TestRefineRoomList(t *testing.T) {
	// Outdoor room moving storm -> rain (stale storm stripped, rain added).
	f := newFake()
	refineRoomList(f, []string{"weather-storm"}, "rain", false)
	want := []string{"remove:weather-storm", "add:weather-rain"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}

	// Indoor room with a stale OUTDOOR id heals to the indoor variant.
	f = newFake()
	refineRoomList(f, []string{"weather-rain"}, "rain", true)
	want = []string{"remove:weather-rain", "add:weather-rain-indoor"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}

	// Clear strips everything.
	f = newFake()
	refineRoomList(f, []string{"weather-rain-indoor"}, sim.Clear, true)
	want = []string{"remove:weather-rain-indoor"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}
}
