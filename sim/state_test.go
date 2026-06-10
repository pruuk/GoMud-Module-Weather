package sim

import (
	"reflect"
	"testing"
)

func TestStateJSONRoundTrip(t *testing.T) {
	st := State{
		Round:    7,
		RNGState: 0xDEADBEEF,
		NextID:   3,
		Fronts: []Front{
			{Id: 1, Type: "storm", Zone: "A", Intensity: 0.5, Moisture: 0.6, Age: 4, MaxAge: 24, History: []ZoneId{"B", "A"}},
			{Id: 2, Type: "fog", Zone: "C", Intensity: 0.2, Moisture: 0.9, Age: 1, MaxAge: 12, History: nil},
		},
		Weather: map[ZoneId]WeatherType{"A": "storm", "B": Clear, "C": "fog"},
	}
	b, err := st.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	got, err := StateFromJSON(b)
	if err != nil {
		t.Fatalf("StateFromJSON: %v", err)
	}
	if !reflect.DeepEqual(st, got) {
		t.Errorf("round trip mismatch:\n want %+v\n got  %+v", st, got)
	}
}

func TestStateFromJSONError(t *testing.T) {
	if _, err := StateFromJSON([]byte("{bad")); err == nil {
		t.Error("StateFromJSON should error on malformed JSON")
	}
}
