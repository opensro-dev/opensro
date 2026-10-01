/*
===========================================================================

door_persistence_test.go - mutations survive a restart through the authority store

===========================================================================
*/

package action

// Store-backed integration tests for the ADR-1 commit door (P3's L2 test
// manifest, D11): every action mutation site commits through a REAL
// authority store and must survive a simulated watchdog reboot - a FRESH
// store hydrated from the state file alone (the XDROP-5 anti-vacuous bar:
// nothing in-process may satisfy the asserts).
//
// The composition under test is exactly the server wiring's:
// store.Open -> Deps{Characters: store.Characters(), MutateCharacter:
// store.Mutate} -> NewRuntime -> store.AttachGround(rt.Ground) ->
// rt.Ground.Restore(store.GroundSnapshotForRestore()).

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
doorSkillSeeder

doorSkillSeeder is this suite's stand-in for enterworld.DefaultSkillSeeder
(the store's unconditional creation-seed invariant refuses an unseeded
CreateCharacter): the same racial id sets, without a textdata dependency.
==================
*/
func doorSkillSeeder(raceKey string, learned []uint32) ([]uint32, error) {
	ids := []uint32{1, 7127, 7128, 7129, 7909, 7910, 8454, 9069, 9606, 9970}
	if raceKey == enterworld.RaceKeyChina {
		ids = []uint32{1, 2, 40, 70}
	}
	have := make(map[uint32]bool, len(learned))
	for _, id := range learned {
		have[id] = true
	}
	missing := make([]uint32, 0, len(ids))
	for _, id := range ids {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// doorRuntime is one store-backed runtime composition.
type doorRuntime struct {
	rt        *Runtime
	authority *store.Store
	clock     *fakeClock
	character *enterworld.Character
	dir       string
}

/*
==================
openDoorRuntime

openDoorRuntime opens (or reopens) the authority store in dir and wires
a Runtime whose door commits through it. When seed is non-nil AND the
store holds no character yet, seed installs as a new record (first boot);
on a reopen the character comes back from the FILE, never from seed.
==================
*/
func openDoorRuntime(t *testing.T, dir string, seed *enterworld.Character) *doorRuntime {
	t.Helper()
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dir, err)
	}
	t.Cleanup(authority.Close)
	existing := authority.Characters().CharactersForDivision(testDivision)
	if len(existing) == 0 && seed != nil {
		if err := authority.CreateCharacter(testDivision, "test-account", seed); err != nil {
			t.Fatalf("CreateCharacter: %v", err)
		}
		existing = authority.Characters().CharactersForDivision(testDivision)
	}
	// The store seeds racial base skills at creation; the stat projection
	// needs their reference rows (potion amounts read the keeper).
	skills := staticSkillSource{}
	for _, id := range []uint32{1, 2, 40, 70, 7127, 7128, 7129, 7909, 7910, 8454, 9069, 9606, 9970} {
		skills[id] = enterworld.SkillRow{ID: id}
	}
	deps := &enterworld.Deps{
		Characters: authority.Characters(),
		Items:      testItems(),
		Skills:     skills,
	}
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		// The server wiring's scoped door (ADR-2).
		authority.MutateCharacter(c, label, fn)
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	authority.AttachGround(rt.Ground)
	rt.Ground.Restore(authority.GroundSnapshotForRestore())

	var character *enterworld.Character
	if len(existing) > 0 {
		character = existing[0]
	}
	return &doorRuntime{rt: rt, authority: authority, clock: clock, character: character, dir: dir}
}

/*
==================
reboot

reboot simulates the watchdog cycle: a completely fresh composition
hydrated from the directory alone. The outgoing store closes first -
otherwise every reboot in a chain stacks another live SQLite handle on
the same path while the fresh Open runs its bak VACUUM (Close is
idempotent, so the t.Cleanup registered at open no-ops later). A test
that must hydrate BESIDE a still-live store calls openDoorRuntime
directly instead.
==================
*/
func (d *doorRuntime) reboot(t *testing.T) *doorRuntime {
	t.Helper()
	d.authority.Close()
	return openDoorRuntime(t, d.dir, nil)
}

func bagRowByCodename(c *enterworld.Character, codename string) *enterworld.InventoryRow {
	for i := range c.MissionInventory {
		if c.MissionInventory[i].Codename == codename {
			return &c.MissionInventory[i]
		}
	}
	return nil
}

func groundByRefObjID(rt *Runtime, refObjID uint32) *grounditem.Item {
	for _, item := range rt.Ground.All(testDivision) {
		if item.RefObjID == refObjID {
			found := item
			return &found
		}
	}
	return nil
}

func goldOfT(t *testing.T, c *enterworld.Character) int64 {
	t.Helper()
	if c.Gold == nil {
		t.Fatal("character has no gold field")
	}
	return *c.Gold
}

/*
==================
TestEveryMutationSiteSurvivesRestart

TestEveryMutationSiteSurvivesRestart drives each action mutation site
(ADR-1 census S2-S7) through the real handler against a real store, then
proves a fresh hydration equals the post-mutation state, never the
pre-mutation one.
==================
*/
func TestEveryMutationSiteSurvivesRestart(t *testing.T) {
	t.Parallel()
	t.Run("S2 inventory move survives", func(t *testing.T) {
		t.Parallel()
		d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
		result := d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeInventory,
			SourceSlot:   20, DestSlot: 25, Quantity: 1,
		}))
		assertOpcodes(t, result.Frames, wire.OpItemMoveResponse)

		r := d.reboot(t)
		row := bagRowByCodename(r.character, "ITEM_CH_SWORD_01_A_RARE")
		if row == nil || row.Slot != 25 {
			t.Fatalf("after reboot sword row = %+v, want slot 25 (post-mutation, not pre)", row)
		}
		if row.VarianceBits != "9223372036854775808" {
			t.Fatalf("variance after reboot = %q, want the 2^63 decimal string byte-exact", row.VarianceBits)
		}
	})

	t.Run("S3 ground drop survives with gid continuity", func(t *testing.T) {
		t.Parallel()
		d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
		result := d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20,
		}))
		assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpSingleObjectSpawn)
		dropped := groundByRefObjID(d.rt, 11459)
		if dropped == nil {
			t.Fatal("drop never reached the registry")
		}

		r := d.reboot(t)
		if bagRowByCodename(r.character, "ITEM_CH_SWORD_01_A_RARE") != nil {
			t.Fatal("after reboot the dropped sword is still in the bag (pre-mutation state resurrected)")
		}
		restored := groundByRefObjID(r.rt, 11459)
		if restored == nil {
			t.Fatal("after reboot the drop is gone from the ground (mutation lost)")
		}
		if restored.Gid != dropped.Gid {
			t.Fatalf("ground gid changed across reboot: %d -> %d", dropped.Gid, restored.Gid)
		}
		if restored.VarianceBits != 0x8000000000000000 {
			t.Fatalf("ground variance = %d, want 2^63 (string passthrough)", restored.VarianceBits)
		}
		if restored.DroppedAt.IsZero() {
			t.Fatal("after reboot the drop lost its DroppedAt (would never expire)")
		}
		// gid continuity: a post-reboot drop allocates ABOVE the restored one.
		next := r.rt.Ground.Add(testDivision, grounditem.Item{RefObjID: 1, TypeFlags: 0x08AC})
		if next.Gid <= restored.Gid {
			t.Fatalf("post-reboot gid %d not above restored %d", next.Gid, restored.Gid)
		}
	})

	t.Run("S4 gold drop survives both planes", func(t *testing.T) {
		t.Parallel()
		d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
		result := d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGoldDrop, GoldAmount: 1500,
		}))
		assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpPointsUpdate, wire.OpSingleObjectSpawn)

		r := d.reboot(t)
		if got := goldOfT(t, r.character); got != 3500 {
			t.Fatalf("gold after reboot = %d, want 3500 (debited)", got)
		}
		heap := groundByRefObjID(r.rt, 62)
		if heap == nil || heap.GoldAmount != 1500 {
			t.Fatalf("heap after reboot = %+v, want 1500 gold", heap)
		}
	})

	t.Run("S6 gold pickup survives both planes", func(t *testing.T) {
		t.Parallel()
		d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
		d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGoldDrop, GoldAmount: 1500,
		}))
		heap := groundByRefObjID(d.rt, 62)
		if heap == nil {
			t.Fatal("gold drop setup failed")
		}
		grant := d.rt.HandleTargetInteract(testDivision, d.character, wire.TargetInteract{Gid: heap.Gid}.Encode())
		assertOpcodes(t, grant.Frames,
			wire.OpActionState, wire.OpPickupAnim, wire.OpItemMoveResponse,
			wire.OpPointsUpdate, wire.OpObjectDespawn)

		r := d.reboot(t)
		if got := goldOfT(t, r.character); got != 5000 {
			t.Fatalf("gold after reboot = %d, want the re-credited 5000", got)
		}
		if r.rt.Ground.Count(testDivision) != 0 {
			t.Fatal("picked heap resurrected across the reboot")
		}
	})

	t.Run("S7 item pickup survives both planes", func(t *testing.T) {
		t.Parallel()
		d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
		d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20,
		}))
		dropped := groundByRefObjID(d.rt, 11459)
		if dropped == nil {
			t.Fatal("drop setup failed")
		}
		grant := d.rt.HandleTargetInteract(testDivision, d.character, wire.TargetInteract{Gid: dropped.Gid}.Encode())
		assertOpcodes(t, grant.Frames,
			wire.OpActionState, wire.OpPickupAnim, wire.OpItemMoveResponse,
			wire.OpObjectDespawn)

		r := d.reboot(t)
		row := bagRowByCodename(r.character, "ITEM_CH_SWORD_01_A_RARE")
		if row == nil {
			t.Fatal("after reboot the picked sword is not in the bag")
		}
		if row.VarianceBits != "9223372036854775808" {
			t.Fatalf("variance after ground round-trip = %q, want 2^63 byte-exact", row.VarianceBits)
		}
		if r.rt.Ground.Count(testDivision) != 0 {
			t.Fatal("picked item resurrected on the ground")
		}
	})

	t.Run("S7 over-cap remainder survives with its gid", func(t *testing.T) {
		t.Parallel()
		seed := testCharacter()
		seed.MissionInventory = append(seed.MissionInventory, enterworld.InventoryRow{
			Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), VarianceBits: "0", StackCount: 40,
		})
		seed.MissionInventory = append(seed.MissionInventory, enterworld.InventoryRow{
			Slot: 22, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
			TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), VarianceBits: "0", StackCount: 30,
		})
		d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), seed)

		// Drop the 40-stack; the 30-stack stays in the bag (cap 50).
		d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGroundDrop, SourceSlot: 21,
		}))
		heap := groundByRefObjID(d.rt, 3630)
		if heap == nil || heap.StackCount != 40 {
			t.Fatalf("heap setup = %+v, want 40 potions", heap)
		}
		grant := d.rt.HandleTargetInteract(testDivision, d.character, wire.TargetInteract{Gid: heap.Gid}.Encode())
		if grant.Pending != nil {
			t.Fatalf("pickup pended (%+v), want an in-place grant", grant.Pending)
		}

		r := d.reboot(t)
		row := bagRowByCodename(r.character, "ITEM_ETC_HP_POTION_01")
		if row == nil || row.StackCount != 50 {
			t.Fatalf("bag stack after reboot = %+v, want capped 50", row)
		}
		remainder := groundByRefObjID(r.rt, 3630)
		if remainder == nil || remainder.StackCount != 20 {
			t.Fatalf("ground remainder after reboot = %+v, want 20", remainder)
		}
		if remainder.Gid != heap.Gid {
			t.Fatalf("remainder gid changed across reboot: %d -> %d", heap.Gid, remainder.Gid)
		}
	})

	t.Run("S5 approach world write survives", func(t *testing.T) {
		t.Parallel()
		d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
		start := simulation.SeedWorldState(d.character).Spawn
		heap := d.rt.Ground.Add(testDivision, PlanGoldDrop(
			GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2},
			1000,
			// 100u east: out of reach, so the interact arms an approach.
			// (Registry.Add outside the door is test SETUP; the site under
			// test is the approach's own commit.)
			simulation.Spawn{RegionID: start.RegionID, X: start.X + 100, Y: start.Y, Z: start.Z},
			"someone", d.clock.Now()))

		armed := d.rt.HandleTargetInteract(testDivision, d.character, wire.TargetInteract{Gid: heap.Gid}.Encode())
		if armed.Pending == nil {
			t.Fatalf("interact = %+v, want an armed approach", armed)
		}

		r := d.reboot(t)
		world := r.character.World
		if world == nil || world.Spawn == nil || world.Spawn.X == nil {
			t.Fatalf("world after reboot = %+v, want the approach goal", world)
		}
		// The approach goal is the GROUND ITEM's position, which lives as
		// float32 in the registry (grounditem.Point) - the persisted goal
		// carries that truncation by design.
		wantX := float64(float32(start.X + 100))
		if *world.Spawn.X != wantX {
			t.Fatalf("goal X after reboot = %v, want %v (the approach's destination)", *world.Spawn.X, wantX)
		}
		if !world.SpawnSet {
			t.Fatal("spawnSet lost across reboot")
		}
		// The live plane reseeds AT the goal (moveSegment is runtime-only
		// by design in both stacks; a kill mid-walk resumes at the goal).
		reseeded := simulation.SeedWorldState(r.character)
		if reseeded.Spawn.X != wantX {
			t.Fatalf("reseeded live X = %v, want the goal %v", reseeded.Spawn.X, wantX)
		}
	})

	t.Run("ttl sweep removal survives", func(t *testing.T) {
		t.Parallel()
		d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
		d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20,
		}))
		if d.rt.Ground.Count(testDivision) != 1 {
			t.Fatal("drop setup failed")
		}
		frames := d.rt.SweepExpired(d.clock.At(grounditem.FixtureLifetime).UnixMilli())
		if len(frames) != 1 {
			t.Fatalf("sweep = %+v, want one division burst", frames)
		}

		r := d.reboot(t)
		if got := r.rt.Ground.Count(testDivision); got != 0 {
			t.Fatalf("after reboot %d ground item(s) resurrected past their TTL", got)
		}
	})
}

/*
==================
TestGroundTTLContinuityThroughAuthorityStore

TestGroundTTLContinuityThroughAuthorityStore proves the wall-clock
deadline contract through the NEW layer (supersedes-in-coverage DROP-3's
grounditem-local test once the absorb lands): the ORIGINAL DroppedAt
rides the store, the deadline does not extend across a reboot, and a
never-expires row stays never-expires.
==================
*/
func TestGroundTTLContinuityThroughAuthorityStore(t *testing.T) {
	t.Parallel()
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
	droppedAt := d.clock.Now()
	d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20,
	}))
	// A never-expires row (no timestamp) planted through the door.
	d.rt.deps.Mutate(nil, "ttl-sweep", func() {
		d.rt.Ground.Add(testDivision, grounditem.Item{RefObjID: 777, TypeFlags: 0x08AC})
	})

	r := d.reboot(t)
	restored := groundByRefObjID(r.rt, 11459)
	if restored == nil {
		t.Fatal("timed drop missing after reboot")
	}
	if !restored.DroppedAt.Equal(droppedAt) {
		t.Fatalf("DroppedAt after reboot = %v, want the ORIGINAL %v (deadline must not extend)", restored.DroppedAt, droppedAt)
	}

	// 1ms shy of the original deadline: nothing expires.
	if frames := r.rt.SweepExpired(droppedAt.Add(grounditem.FixtureLifetime - time.Millisecond).UnixMilli()); len(frames) != 0 {
		t.Fatalf("sweep before the original deadline expired %+v", frames)
	}
	// The first sweep slot past the deadline (sweeps run on the 5s
	// SweepInterval cadence): the timed drop reaps, the untimed survives.
	if frames := r.rt.SweepExpired(droppedAt.Add(grounditem.FixtureLifetime + grounditem.SweepInterval).UnixMilli()); len(frames) != 1 {
		t.Fatalf("first sweep past the original deadline = %+v, want the timed drop", frames)
	}
	if groundByRefObjID(r.rt, 777) == nil {
		t.Fatal("the never-expires row was reaped")
	}

	// Second reboot: the never-expires row still never expires.
	r2 := r.reboot(t)
	if frames := r2.rt.SweepExpired(droppedAt.Add(100 * grounditem.FixtureLifetime).UnixMilli()); len(frames) != 0 {
		t.Fatalf("never-expires row expired after a reboot: %+v", frames)
	}
}

/*
==================
TestTwoPlaneOpsNeverTearOnDisk

TestTwoPlaneOpsNeverTearOnDisk is the door's referee (ADR-1 D11): during
every two-plane op the committed store must never carry a half-applied
intermediate - each committed generation hydrates to exactly pre-op or
post-op (item exactly-once across bag+ground, gold conserved). The old
"no write before the commit point" byte-watch is structural now: the
engine's ONLY writer is the commit transaction itself, so a mid-closure
intermediate is unrepresentable (a reader would see the previous
committed generation by WAL snapshot isolation).
==================
*/
func TestTwoPlaneOpsNeverTearOnDisk(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "authority")
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(authority.Close)
	if err := authority.CreateCharacter(testDivision, "test-account", testCharacter()); err != nil {
		t.Fatalf("CreateCharacter: %v", err)
	}
	character := authority.Characters().CharactersForDivision(testDivision)[0]

	generationsDir := t.TempDir()
	var commits []string
	deps := &enterworld.Deps{Characters: authority.Characters(), Items: testItems()}
	deps.MutateCharacter = func(c *enterworld.Character, label string, fn func()) {
		authority.MutateCharacter(c, label, fn)
		// Witness the committed generation: a consistent snapshot taken
		// right after the op's commit.
		generation := filepath.Join(generationsDir, fmt.Sprintf("gen-%d.db", len(commits)))
		if err := authority.BackupTo(generation); err != nil {
			t.Fatalf("op %q generation snapshot: %v", label, err)
		}
		commits = append(commits, generation)
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	rt.Now = clock.Now
	authority.AttachGround(rt.Ground)
	rt.Ground.Restore(authority.GroundSnapshotForRestore())

	// Drive all four two-plane ops: S3 drop, S7 pickup, S4 gold drop,
	// S6 gold pickup.
	rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20,
	}))
	sword := groundByRefObjID(rt, 11459)
	if sword == nil {
		t.Fatal("drop missing")
	}
	rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Gid: sword.Gid}.Encode())
	rt.HandleItemMove(testDivision, character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGoldDrop, GoldAmount: 1500,
	}))
	heap := groundByRefObjID(rt, 62)
	if heap == nil {
		t.Fatal("gold drop missing")
	}
	rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Gid: heap.Gid}.Encode())

	if len(commits) != 4 {
		t.Fatalf("recorded %d commits, want exactly 4 (one per op - no-op commits and double writes both violate the door discipline)", len(commits))
	}

	// EVERY committed generation hydrates whole: exactly-once + conservation.
	for index, generation := range commits {
		payload, err := os.ReadFile(generation)
		if err != nil {
			t.Fatal(err)
		}
		hydrateDir := filepath.Join(t.TempDir(), fmt.Sprintf("gen-%d", index))
		if err := os.MkdirAll(hydrateDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hydrateDir, store.DBFileName), payload, 0o644); err != nil {
			t.Fatal(err)
		}
		hydrated, err := store.Open(hydrateDir, store.Options{})
		if err != nil {
			t.Fatalf("generation %d does not hydrate: %v", index, err)
		}
		defer hydrated.Close()
		chars := hydrated.Characters().CharactersForDivision(testDivision)
		if len(chars) != 1 {
			t.Fatalf("generation %d holds %d characters", index, len(chars))
		}
		ground := hydrated.GroundSnapshotForRestore()

		swordInBag := 0
		if bagRowByCodename(chars[0], "ITEM_CH_SWORD_01_A_RARE") != nil {
			swordInBag = 1
		}
		swordOnGround := 0
		goldOnGround := int64(0)
		for _, rows := range ground.Divisions {
			for _, row := range rows {
				if row.RefObjID == 11459 {
					swordOnGround++
				}
				goldOnGround += int64(row.GoldAmount)
			}
		}
		if swordInBag+swordOnGround != 1 {
			t.Errorf("generation %d: sword count = bag %d + ground %d, want exactly 1 (torn two-plane state)", index, swordInBag, swordOnGround)
		}
		if total := goldOfT(t, chars[0]) + goldOnGround; total != 5000 {
			t.Errorf("generation %d: gold total = %d, want 5000 conserved", index, total)
		}
	}
}

/*
==================
TestPersistFailureLoudAndNonFatal

TestPersistFailureLoudAndNonFatal proves ADR-1 D5's fail-open contract at
the integration level: a write outage never refuses the op or corrupts
the last committed generation, Health degrades loudly, and the next
successful commit self-heals with ALL accumulated state.
==================
*/
func TestPersistFailureLoudAndNonFatal(t *testing.T) {
	t.Parallel()
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())

	outage := errors.New("disk on fire")
	d.authority.FailCommits(outage)

	result := d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20,
	}))
	// FAIL OPEN: the op acked (frames shipped) and applied in memory.
	assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpSingleObjectSpawn)
	if d.rt.Ground.Count(testDivision) != 1 {
		t.Fatal("the op did not apply in memory during the outage")
	}
	health := d.authority.Health()
	if health.FailedWrites == 0 || health.LastError == "" {
		t.Fatalf("health = %+v, want a loud degradation", health)
	}
	if !strings.Contains(health.LastError, "ground-drop") {
		t.Errorf("health.LastError = %q, want the op label for attribution", health.LastError)
	}
	// STALE-NOT-TORN: the committed generation during the outage is the
	// intact PRE-OP one (the sword still in the bag, nothing on the
	// ground), never a damaged or half-applied one. The degraded store
	// must stay OPEN here - it still carries the outage-window state the
	// healing commit below has to flush - and the single-writer guard
	// refuses a second live store over its directory, so the witness is
	// a consistent snapshot hydrated in a FRESH directory (the same
	// generation witness TestTwoPlaneOpsNeverTearOnDisk rides).
	staleDir := filepath.Join(t.TempDir(), "stale")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := d.authority.BackupTo(filepath.Join(staleDir, store.DBFileName)); err != nil {
		t.Fatalf("outage-window generation snapshot: %v", err)
	}
	stale := openDoorRuntime(t, staleDir, nil)
	if bagRowByCodename(stale.character, "ITEM_CH_SWORD_01_A_RARE") == nil {
		t.Fatal("the outage hydration lost the pre-op bag row (last good generation damaged)")
	}
	if stale.rt.Ground.Count(testDivision) != 0 {
		t.Fatal("the outage hydration carries a half-applied ground plane")
	}
	stale.authority.Close()

	// Outage ends: the next commit self-heals with BOTH ops' state.
	d.authority.FailCommits(nil)
	d.rt.HandleItemMove(testDivision, d.character, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeGoldDrop, GoldAmount: 1500,
	}))
	if health := d.authority.Health(); health.FailedWrites != 0 {
		t.Fatalf("health after recovery = %+v, want clean", health)
	}

	r := d.reboot(t)
	if bagRowByCodename(r.character, "ITEM_CH_SWORD_01_A_RARE") != nil {
		t.Fatal("the outage-window drop was lost by the healing commit")
	}
	if groundByRefObjID(r.rt, 11459) == nil {
		t.Fatal("the outage-window ground item was lost by the healing commit")
	}
	if got := goldOfT(t, r.character); got != 3500 {
		t.Fatalf("gold after recovery reboot = %d, want 3500", got)
	}
}

// Burst-helper environment keys (TestKillMidBurstNeverTearsState re-exec).
const (
	envBurstHelper = "SRO_P3_BURST_HELPER"
	envBurstDir    = "SRO_P3_BURST_DIR"
)

/*
==================
TestKillMidBurstNeverTearsState

TestKillMidBurstNeverTearsState is the 16m42s killer in miniature: a
child process storms two-plane mutations against a real store and is
TerminateProcess-killed at a random instant; the hydrated survivor state
must be whole at EVERY kill point - exactly-once items, conserved gold,
no recovery-ladder engagement (rename atomicity held), no quarantines.
==================
*/
func TestKillMidBurstNeverTearsState(t *testing.T) {
	if os.Getenv(envBurstHelper) == "1" {
		burstHelperMain(t)
		return
	}
	if testing.Short() {
		t.Skip("child-process kill storm")
	}
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// The kill runs stay SEQUENTIAL: spawning and TerminateProcess-killing
	// several copies of this test binary concurrently leaks inherited
	// pipe/exe handles between the children on Windows, which wedges
	// child.Wait() and strands zombie children. The parent's t.Parallel
	// above is what buys the wall time back, by overlapping this whole
	// storm with the wire-delivery tests.
	const runs = 6
	for i := 0; i < runs; i++ {
		killAfter := time.Duration(30+rng.Intn(320)) * time.Millisecond
		t.Run(fmt.Sprintf("kill-%d", i), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "authority")

			child := exec.Command(exe, "-test.run", "^TestKillMidBurstNeverTearsState$", "-test.v")
			child.Env = append(os.Environ(), envBurstHelper+"=1", envBurstDir+"="+dir)
			var childOut bytes.Buffer
			child.Stdout, child.Stderr = &childOut, &childOut
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(killAfter) //nolint:forbidigo // fault injection: kill the child at a random wall-clock moment
			if err := child.Process.Kill(); err != nil {
				t.Fatalf("kill: %v (child output: %s)", err, childOut.String())
			}
			_ = child.Wait()

			// Clear the dead child's lock: on this machine PIDs recycle
			// within milliseconds (parallel compilers), so pidAlive can
			// see a REUSED pid as the owner and refuse - the documented
			// lockfile residual. Wait() above is our proof of death, so
			// removing the lock here is exactly the documented operator
			// action, not a weakening of the dual-writer guard.
			_ = os.Remove(filepath.Join(dir, "authority.lock"))

			// Hydrate the survivor. A kill before the very first commit
			// legitimately leaves no state file - that is the empty first
			// boot, not a tear.
			survivor, err := store.Open(dir, store.Options{})
			if err != nil {
				t.Fatalf("survivor store does not open: %v (child output: %s)", err, childOut.String())
			}
			defer survivor.Close()
			health := survivor.Health()
			if health.LoadedFromBak {
				t.Fatalf("survivor recovered from .bak - the main file tore under kill (child output: %s)", childOut.String())
			}
			quarantines, _ := filepath.Glob(filepath.Join(dir, store.DBFileName+".corrupt-*"))
			if len(quarantines) != 0 {
				t.Fatalf("kill produced quarantine artifacts: %v", quarantines)
			}

			chars := survivor.Characters().CharactersForDivision(testDivision)
			if len(chars) == 0 {
				return // killed before the seed commit: clean empty store.
			}
			c := chars[0]
			ground := survivor.GroundSnapshotForRestore()

			swordCount, potionCount, groundGold := 0, int64(0), int64(0)
			for _, row := range c.MissionInventory {
				switch row.RefObjID {
				case 11459:
					swordCount++
				case 3630:
					potionCount += row.StackCount
				}
			}
			for _, rows := range ground.Divisions {
				for _, row := range rows {
					switch row.RefObjID {
					case 11459:
						swordCount++
					case 3630:
						potionCount += int64(row.StackCount)
					}
					groundGold += int64(row.GoldAmount)
					if row.GoldAmount == 0 && row.DroppedAtMs == 0 {
						t.Errorf("ground row %d lost its DroppedAt under kill", row.Gid)
					}
				}
			}
			if swordCount != 1 {
				t.Errorf("sword exactly-once violated: %d copies (dupe or loss under kill)", swordCount)
			}
			if potionCount != 40 {
				t.Errorf("potion conservation violated: %d, want 40", potionCount)
			}
			if goldTotal := goldOfT(t, c) + groundGold; goldTotal != 5000 {
				t.Errorf("gold conservation violated: %d, want 5000", goldTotal)
			}
		})
	}
}

/*
==================
burstHelperMain

burstHelperMain is the child body: seed once, then storm two-plane ops
until the parent kills us. Every op commits through the real door; the
loop never exits voluntarily (a 30s guard fails loudly if the parent
forgot us).
==================
*/
func burstHelperMain(t *testing.T) {
	dir := os.Getenv(envBurstDir)
	if dir == "" {
		t.Fatal("burst helper without " + envBurstDir)
	}
	authority, err := store.Open(dir, store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatalf("helper store.Open: %v", err)
	}
	seed := testCharacter()
	seed.MissionInventory = append(seed.MissionInventory, enterworld.InventoryRow{
		Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
		TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), VarianceBits: "0", StackCount: 40,
	})
	if err := authority.CreateCharacter(testDivision, "test-account", seed); err != nil {
		t.Fatalf("helper seed: %v", err)
	}
	character := authority.Characters().CharactersForDivision(testDivision)[0]

	deps := &enterworld.Deps{Characters: authority.Characters(), Items: testItems()}
	deps.MutateCharacter = func(_ *enterworld.Character, label string, fn func()) {
		authority.Mutate(label, fn)
	}
	rt := NewRuntime(deps, nil)
	authority.AttachGround(rt.Ground)
	rt.Ground.Restore(authority.GroundSnapshotForRestore())

	dropBySlotOf := func(refObjID uint32) {
		for _, row := range character.MissionInventory {
			if row.RefObjID == refObjID {
				payload, _ := wire.ItemMoveRequest{
					MovementType: wire.MoveTypeGroundDrop, SourceSlot: uint8(row.Slot),
				}.Encode()
				rt.HandleItemMove(testDivision, character, payload)
				return
			}
		}
	}
	pickupOf := func(refObjID uint32) {
		for _, item := range rt.Ground.All(testDivision) {
			if item.RefObjID == refObjID {
				rt.HandleTargetInteract(testDivision, character, wire.TargetInteract{Gid: item.Gid}.Encode())
				return
			}
		}
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		dropBySlotOf(11459) // sword out
		pickupOf(11459)     // sword back
		dropBySlotOf(3630)  // potions out
		pickupOf(3630)      // potions back
		goldOut, _ := wire.ItemMoveRequest{MovementType: wire.MoveTypeGoldDrop, GoldAmount: 1500}.Encode()
		rt.HandleItemMove(testDivision, character, goldOut)
		pickupOf(62) // gold back
	}
	t.Fatal("burst helper survived 30s - the parent never killed it")
}
