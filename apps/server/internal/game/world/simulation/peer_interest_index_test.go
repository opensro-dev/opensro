package simulation

import (
	"fmt"
	"math/rand"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"slices"
	"testing"
)

func exhaustivePeerCandidates(sessions []SessionSnapshot, viewer *SessionSnapshot, now int64, pets bool, out []int) []int {
	out = out[:0]
	for i := range sessions {
		peer := &sessions[i]
		if peer.Appearance == nil || peer.DivisionID != viewer.DivisionID || peer.WorldInstance != viewer.WorldInstance {
			continue
		}
		world := peer.World
		if pets {
			if peer.COS == nil || peer.COS.Row.Gid == 0 {
				continue
			}
			world = peer.COS.World
		}
		if peerPositionVisible(viewer.World.LiveSpawnAt(now), world.LiveSpawnAt(now)) {
			out = append(out, i)
		}
	}
	return out
}

func TestPeerInterestIndexMatchesExhaustiveVisibility(t *testing.T) {
	random := rand.New(rand.NewSource(8173))
	sessions := make([]SessionSnapshot, 400)
	for i := range sessions {
		s := peerSession(fmt.Sprint(i), fmt.Sprint(i%2), int64(i+1), "peer")
		s.WorldInstance = uint32(i % 3)
		// Includes negative coordinates, seam-adjacent blocks and dungeon regions.
		s.World.Spawn = Spawn{RegionID: []uint16{0x6060, 0x6061, 0x6160, 0x8001, 0x8002}[i%5], X: float64(random.Intn(9)-2) * 320, Z: float64(random.Intn(9)-2) * 320}
		if i%7 == 0 {
			s.Appearance = nil
		}
		if i%3 != 0 {
			s.COS = &PeerCOS{Row: wire.CosSpawnBand2{Gid: uint32(i + 1)}, World: s.World}
			s.COS.World.Spawn.X += 321
		}
		sessions[i] = s
	}
	for _, pets := range []bool{false, true} {
		index := buildPeerInterestIndex(sessions, 1000, pets)
		var got, want []int
		for i := range sessions {
			viewer := &sessions[i]
			got = index.candidates(viewer, viewer.World.LiveSpawnAt(1000), got)
			want = exhaustivePeerCandidates(sessions, viewer, 1000, pets, want)
			if !slices.Equal(got, want) {
				t.Fatalf("viewer %d pets %v: got %v want %v", i, pets, got, want)
			}
		}
	}
}

func TestPeerVisibilityChangeKeysCoverPublicationInputs(t *testing.T) {
	base := peerSession("session", "world", 1, "peer")
	base.World.Spawn = Spawn{RegionID: 0x6060, X: 10, Z: 10}
	tests := []struct {
		name   string
		change func(*SessionSnapshot)
		want   bool
	}{
		{"same-block movement", func(s *SessionSnapshot) { s.World.Spawn.X++ }, false},
		{"block crossing", func(s *SessionSnapshot) { s.World.Spawn.X += 320 }, true},
		{"reconnect", func(s *SessionSnapshot) { s.SessionID = "new-scene" }, true},
		{"character replacement", func(s *SessionSnapshot) { s.CharacterID++ }, true},
		{"world transfer", func(s *SessionSnapshot) { s.WorldInstance++ }, true},
		{"division transfer", func(s *SessionSnapshot) { s.DivisionID = "other" }, true},
		{"appearance unavailable", func(s *SessionSnapshot) { s.Appearance = nil }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &divisionTickState{}
			live := map[string]bool{base.SessionID: true}
			if !state.peerVisibilityChanged([]SessionSnapshot{base}, 0, live) {
				t.Fatal("first pass skipped")
			}
			state.peerVisibilityValid = true
			next := base
			tt.change(&next)
			if got := state.peerVisibilityChanged([]SessionSnapshot{next}, 0, live); got != tt.want {
				t.Fatalf("changed %v want %v", got, tt.want)
			}
			if !state.peerVisibilityChanged(nil, 0, nil) {
				t.Fatal("disconnect skipped")
			}
			if len(state.peerVisibilityKeys) != 0 {
				t.Fatal("departed viewer retained")
			}
		})
	}
}

type firstPushPanic struct {
	fakePusher
	fail bool
}

func (p *firstPushPanic) PushToSession(id string, frames []Frame) {
	if p.fail {
		p.fail = false
		panic("injected publication failure")
	}
	p.fakePusher.PushToSession(id, frames)
}
func TestPeerVisibilityRetriesUncommittedPass(t *testing.T) {
	sessions := []SessionSnapshot{peerSession("a", "world", 1, "a"), peerSession("b", "world", 2, "b")}
	live := map[string]bool{"a": true, "b": true}
	state := &divisionTickState{shownPeers: make(map[string]map[uint32]bool)}
	push := &firstPushPanic{fail: true}
	ticker := &Ticker{Push: push}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("missing injected panic")
			}
		}()
		ticker.runPeerVisibility(state, 0, sessions, live)
	}()
	if state.peerVisibilityValid {
		t.Fatal("partial publication certified")
	}
	ticker.runPeerVisibility(state, 100, sessions, live)
	if !state.peerVisibilityValid || len(peerFramesTo(&push.fakePusher, "a", wire.OpSingleObjectSpawn)) != 1 || len(peerFramesTo(&push.fakePusher, "b", wire.OpSingleObjectSpawn)) != 1 {
		t.Fatal("retry lost or duplicated creates")
	}
}

func BenchmarkPeerInterest1000(b *testing.B) {
	for _, dense := range []bool{false, true} {
		name := "dispersed"
		if dense {
			name = "crowded"
		}
		sessions := make([]SessionSnapshot, 1000)
		for i := range sessions {
			sessions[i] = peerSession(fmt.Sprint(i), "world", int64(i+1), "peer")
			sessions[i].World.Spawn = Spawn{RegionID: 0x6060, X: float64(i%32) * 960, Z: float64(i/32) * 960}
			if dense {
				sessions[i].World.Spawn.X = 10
				sessions[i].World.Spawn.Z = 10
			}
		}
		for _, indexed := range []bool{false, true} {
			mode := "scan"
			if indexed {
				mode = "indexed"
			}
			b.Run(name+"/"+mode, func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					var scratch []int
					var index peerInterestIndex
					if indexed {
						index = buildPeerInterestIndex(sessions, 1000, false)
					}
					count := 0
					for i := range sessions {
						viewer := &sessions[i]
						if indexed {
							scratch = index.candidates(viewer, viewer.World.LiveSpawnAt(1000), scratch)
						} else {
							scratch = exhaustivePeerCandidates(sessions, viewer, 1000, false, scratch)
						}
						count += len(scratch)
					}
					want := 1000
					if dense {
						want = 1000000
					}
					if count != want {
						b.Fatal(count)
					}
				}
			})
		}
	}
}

// Complete steady visibility passes, excluding login/spawn traffic and combat.
// These synthetic snapshots measure this tick phase, not server capacity.
func BenchmarkPeerVisibilitySteady1000(b *testing.B) {
	for _, dense := range []bool{false, true} {
		name := "dispersed"
		if dense {
			name = "crowded"
		}
		sessions := make([]SessionSnapshot, 1000)
		live := make(map[string]bool, 1000)
		state := &divisionTickState{shownPeers: make(map[string]map[uint32]bool)}
		for i := range sessions {
			sessions[i] = peerSession(fmt.Sprint(i), "world", int64(i+1), "peer")
			sessions[i].World.Spawn = Spawn{RegionID: 0x6060, X: float64(i%32) * 960, Z: float64(i/32) * 960}
			if dense {
				sessions[i].World.Spawn.X = 10
				sessions[i].World.Spawn.Z = 10
			}
			live[sessions[i].SessionID] = true
		}
		for i := range sessions {
			shown := make(map[uint32]bool)
			for j := range sessions {
				if i != j && peerPositionVisible(sessions[i].World.Spawn, sessions[j].World.Spawn) {
					shown[PlayerObjectID(sessions[j].CharacterID)] = true
				}
			}
			state.shownPeers[sessions[i].SessionID] = shown
		}
		push := &fakePusher{}
		ticker := &Ticker{Push: push}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				ticker.runPeerVisibility(state, 1000, sessions, live)
				ticker.runPeerCOSVisibility(state, 1000, sessions, live)
			}
			if len(push.toSession) != 0 {
				b.Fatal("steady visibility emitted redundant packets")
			}
		})
	}
}

/*
================
TestPeerInterestIndexMatchesExhaustiveVisibilityWithDungeonGrid

With a dungeon grid installed the index must still equal the exact
predicate: gridded 3x3 dungeon neighbourhoods, positions outside the grid
(Whole blocks that see their whole region) and an ungridded dungeon.
================
*/
func TestPeerInterestIndexMatchesExhaustiveVisibilityWithDungeonGrid(t *testing.T) {
	restore := worldgeom.InstallDungeonMessageGrids(map[uint16]worldgeom.DungeonMessageGrid{
		0x8001: {Min: [3]float64{-320, -1000, -320}, Max: [3]float64{1600, 1000, 1600}},
	})
	defer restore()
	random := rand.New(rand.NewSource(4127))
	sessions := make([]SessionSnapshot, 400)
	for i := range sessions {
		s := peerSession(fmt.Sprint(i), fmt.Sprint(i%2), int64(i+1), "peer")
		s.WorldInstance = uint32(i % 2)
		// 0x8001 positions span -640..1920, so some fall outside its grid.
		s.World.Spawn = Spawn{RegionID: []uint16{0x8001, 0x8001, 0x8001, 0x8002, 0x6060}[i%5], X: float64(random.Intn(9)-2) * 320, Z: float64(random.Intn(9)-2) * 320}
		if i%11 == 0 {
			s.Appearance = nil
		}
		sessions[i] = s
	}
	index := buildPeerInterestIndex(sessions, 1000, false)
	var got, want []int
	for i := range sessions {
		viewer := &sessions[i]
		got = index.candidates(viewer, viewer.World.LiveSpawnAt(1000), got)
		want = exhaustivePeerCandidates(sessions, viewer, 1000, false, want)
		if !slices.Equal(got, want) {
			t.Fatalf("viewer %d at %+v: got %v want %v", i, viewer.World.Spawn, got, want)
		}
	}
}
