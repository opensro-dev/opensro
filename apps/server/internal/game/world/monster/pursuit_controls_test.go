package monster

import "testing"

func TestNativeTraceReasonsAndGates(t *testing.T) {
	c := TacticsControls{TraceBoundary: 1, TraceData: 500}
	for _, tc := range []struct {
		name           string
		distance       float32
		moving, indoor bool
		now, last      uint32
		want           TraceDecision
	}{
		{"recent combat", 100, false, false, 10000, 5001, TraceContinue},
		{"exact five seconds", 100, false, false, 10000, 5000, TraceRun},
		{"stationary boundary", 150, false, false, 10000, 5000, TraceWalk},
		{"moving boundary", 100, true, false, 10000, 5000, TraceWalk},
		{"at trace radius", 500, false, false, 10000, 9999, TraceAbandon},
		{"outside ignores activity", 500.001, false, false, 10000, 9999, TraceAbandon},
		{"indoor trace boundary", 600, false, true, 10000, 9999, TraceAbandon},
		{"indoor outside", 600.001, false, true, 10000, 9999, TraceAbandon},
		{"indoor moving threshold", 200, true, true, 10000, 5000, TraceWalk},
		{"indoor stationary threshold", 249, false, true, 10000, 5000, TraceRun},
		{"timestamp rollover", 100, false, false, 20, ^uint32(0) - 20, TraceContinue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.EvaluateTrace(tc.distance, tc.moving, tc.indoor, tc.now, tc.last); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	for _, flags := range []uint32{4, 128, 132} {
		c.Flags = flags
		if c.EvaluateTrace(1000, false, false, 10000, 0) != TraceContinue {
			t.Fatal("actor bypass lost")
		}
	}
	c.Flags = 0
	for _, boundary := range []uint8{0, 2} {
		c.TraceBoundary = boundary
		if c.TraceEnabled() {
			t.Fatal("disabled boundary scans")
		}
	}
}

func TestAuthoredBaroiCompleteControlsAndPromotion(t *testing.T) {
	pair, ok := populationTacticsControls(t)[evidenceKey("MOB_EU_BAROI_CLON", 26696, 1687.52002, -179.660004, 1829.98999)]
	if !ok {
		t.Fatal("Baroi source join missing")
	}
	c := pair.Normal
	if c.MaxStamina != 150 || c.StaminaVariance != 50 || c.HelpRequest != 2 || c.HelpResponse != 2 || c.DiversionBasis != 5 || c.DiversionBasisData[5] != 30 || c.DiversionKeepBasis != 4 || c.TraceData != 500 || c.TraceBoundary != 1 || c.HomingType != 0 || c.AggressOnHoming != 1 {
		t.Fatalf("lost authored controls: %+v", c)
	}
	nest := NestRow{Controls: c, HasControls: true, Radius: 200, PolicyPinned: true, HasChampionTactics: true, ChampionTactics: ChampionTactics{Controls: pair.Champion, HasControls: true}}
	if ResolveTactics(Instance{Nest: nest}).ChaseLeash != 0 {
		t.Fatal("nest radius still overrides native trace")
	}
	champion := nest.PromoteToChampionTactics(1)
	if !champion.HasControls || champion.Controls.ID != pair.Champion.ID || champion.Controls.ID == c.ID {
		t.Fatal("promotion retained normal controls")
	}
	if champion.Controls != pair.Champion {
		t.Fatal("promotion dropped fields")
	}
}

func TestTacticsControlSchemaRejectsUnknownFields(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unknown fields silently accepted")
		}
	}()
	loadTacticsControls([]byte(`{"Sources":{},"Policies":{"x":{"FutureControl":0}},"Anchors":[]}`))
}

func TestNativeHomingTypeBounds(t *testing.T) {
	for _, tc := range []struct {
		kind     uint8
		distance float32
		inside   bool
	}{
		{0, 199.99, true}, {0, 200, false}, {1, 299.99, true}, {1, 300, false},
		{2, 10000, true}, {255, 10000, true},
	} {
		if got := (TacticsControls{HomingType: tc.kind}).InsideHome(tc.distance, 200); got != tc.inside {
			t.Fatalf("type %d distance %v: %v", tc.kind, tc.distance, got)
		}
	}
}
