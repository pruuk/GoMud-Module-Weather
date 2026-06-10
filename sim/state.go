package sim

import "encoding/json"

// ToJSON serializes the full simulation state (RNG cursor + fronts + resolved
// weather) for persistence.
func (s State) ToJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// StateFromJSON restores a State from its serialized form.
func StateFromJSON(b []byte) (State, error) {
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, err
	}
	return s, nil
}
