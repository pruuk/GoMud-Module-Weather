package content

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// fileNameFor mirrors the engine's util.ConvertForFilename: lowercase,
// apostrophes dropped, any rune outside [a-z0-9] becomes '_'. The plugin
// mutator loader requires each file to be named fileNameFor(mutatorid)+".yaml".
func fileNameFor(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		switch {
		case r == '\'':
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String() + ".yaml"
}

// TestShippedMutatorSpecs validates the data files the engine will load:
// parseable YAML, weather- namespaced ids, loader-compatible filenames, no
// respawnrate (it would fight the orchestrator and block purge-on-remove).
func TestShippedMutatorSpecs(t *testing.T) {
	dir := "../files/datafiles/mutators"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("mutator specs missing: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no mutator specs shipped")
	}
	for _, e := range entries {
		b, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var spec map[string]any
		if err := yaml.Unmarshal(b, &spec); err != nil {
			t.Errorf("%s: bad YAML: %v", e.Name(), err)
			continue
		}
		id, _ := spec["mutatorid"].(string)
		if !strings.HasPrefix(id, "weather-") {
			t.Errorf("%s: mutatorid %q must be weather- namespaced", e.Name(), id)
		}
		if want := fileNameFor(id); e.Name() != want {
			t.Errorf("%s: engine loader requires filename %q for id %q", e.Name(), want, id)
		}
		if _, has := spec["respawnrate"]; has {
			t.Errorf("%s: weather mutators must not set respawnrate", e.Name())
		}
		if _, has := spec["decayrate"]; !has {
			t.Errorf("%s: weather mutators must set decayrate (self-heal safety net)", e.Name())
		}
	}
}
