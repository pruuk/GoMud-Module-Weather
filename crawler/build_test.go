package crawler

import "testing"

func TestBuild_BasicAdjacency(t *testing.T) {
	f := newFakeReader()
	// Zone A: room 1 (1->2 internal), room 2 (2->1 internal, 2->3 crosses to B)
	f.addRoom("A", "plains", 1, true, ExitView{ToRoom: 2})
	f.addRoom("A", "plains", 2, true, ExitView{ToRoom: 1}, ExitView{ToRoom: 3})
	// Zone B: room 3 (3->2 crosses back to A)
	f.addRoom("B", "forest", 3, true, ExitView{ToRoom: 2})

	g, err := Build(f, Options{IncludeSecretExits: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(g.Nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 1 {
		t.Fatalf("want 1 edge, got %d: %+v", len(g.Edges), g.Edges)
	}
	e := g.Edges[0]
	if e.A != "A" || e.B != "B" {
		t.Errorf("want canonical edge A-B, got %s-%s", e.A, e.B)
	}
	// 2->3 (A->B) and 3->2 (B->A) are two distinct crossing exits.
	if e.Weight != 2 {
		t.Errorf("want weight 2, got %d", e.Weight)
	}
}

func TestBuild_UnknownExitTargetIgnored(t *testing.T) {
	f := newFakeReader()
	// Room 1 in A exits to room 99, which belongs to no known zone.
	f.addRoom("A", "plains", 1, true, ExitView{ToRoom: 99})

	g, err := Build(f, Options{IncludeSecretExits: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 0 {
		t.Errorf("dangling exit target should produce no edge, got %+v", g.Edges)
	}
}
