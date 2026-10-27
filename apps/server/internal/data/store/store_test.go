package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
)

// testClock is a controllable clock for deterministic updatedAtMs and
// quarantine names.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.UnixMilli(1_700_000_000_000)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// testSkillSeeder is the store tests' stand-in for
// enterworld.DefaultSkillSeeder: the same racial id sets, without a
// textdata dependency (the codename->id resolution itself is pinned by
// bootstrap's own tests against the shipped table).
func testSkillSeeder(raceKey string, learned []uint32) ([]uint32, error) {
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

func openTest(t testing.TB, dir string, clock *testClock) *Store {
	t.Helper()
	s, err := Open(dir, Options{Now: clock.Now, DefaultSkills: testSkillSeeder})
	if err != nil {
		t.Fatalf("Open(%s): %v", dir, err)
	}
	// Windows cannot delete an open database file; TempDir cleanup needs
	// every handle released.
	t.Cleanup(s.Close)
	return s
}

// readCharacterRecordRaw reads one character's persisted JSON straight
// off the database - the raw-bytes witness the JSON engine's file reads
// used to provide.
func readCharacterRecordRaw(t *testing.T, s *Store, divisionID, nameLower string) string {
	t.Helper()
	if s.db == nil {
		t.Fatal("store has no open database")
	}
	var record string
	if err := s.db.QueryRow("SELECT record FROM characters WHERE division = ? AND name_lower = ?", divisionID, nameLower).Scan(&record); err != nil {
		t.Fatalf("reading record %s/%s: %v", divisionID, nameLower, err)
	}
	return record
}

func int64Ptr(v int64) *int64 { return &v }

// division under test.
const testDivision = "global-official"

// varianceCanary is 2^63 exactly - one past MaxInt64. The live store
// carries it (asd slot 13, the deliberate canary row); any int64 parse
// anywhere corrupts it, so the store treats varianceBits as an opaque
// string and this suite pins those bytes.
const varianceCanary = "9223372036854775808"

func seededCharacter() *enterworld.Character {
	return &enterworld.Character{
		Name: "asd2fixture",
		MissionInventory: []enterworld.InventoryRow{{
			Slot:         13,
			RefObjID:     3117,
			Codename:     "ITEM_EU_SWORD_01_A",
			TypeFlags:    0x00C0,
			Plus:         2,
			VarianceBits: varianceCanary,
			Durability:   96,
			StackCount:   1,
		}},
		Gold: int64Ptr(12345),
		World: &enterworld.CharacterWorld{
			Spawn: &enterworld.WorldSpawn{
				RegionID: int64Ptr(0x6b4f),
				X:        floatPtr(1205.5),
				Y:        floatPtr(80),
				Z:        floatPtr(396.25),
				Angle:    int64Ptr(90),
			},
			MovementMode: int64Ptr(1),
			SpawnSet:     true,
			MoveSegment:  json.RawMessage(`{"startedAtMs":1700000000001,"from":{"x":1,"z":2}}`),
			UpdatedAt:    "2026-07-26T04:00:00.000Z",
		},
		CreatedAt: "2026-06-28T14:49:04.538Z",
	}
}

func floatPtr(v float64) *float64 { return &v }

// TestStateRoundtripFidelity: a store full of the live data's edge shapes
// survives commit -> reopen byte-identically per record. Includes the
// never-seeded nil-inventory witness (nil means the
// first bootstrap seeds it; [] means seeded-and-empty - flipping one to
// the other re-runs or suppresses the starter seed, character.go:77-80)
// and the 2^63 varianceBits canary.
func TestStateRoundtripFidelity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s1 := openTest(t, dir, clock)

	// 12 chars: at the native 2..12 creation ceiling CreateCharacter now
	// enforces.
	neverSeeded := &enterworld.Character{Name: "never_seeded", MissionInventory: nil}
	seeded := seededCharacter()
	seededEmpty := &enterworld.Character{Name: "emptybagfix", MissionInventory: []enterworld.InventoryRow{}}

	for _, c := range []*enterworld.Character{neverSeeded, seeded, seededEmpty} {
		if err := s1.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatalf("CreateCharacter(%s): %v", c.Name, err)
		}
	}
	s1.Close() // a real restart closes the first instance (single-writer guard)

	s2 := openTest(t, dir, clock)
	reloaded := s2.Characters().CharactersForDivision(testDivision)
	if len(reloaded) != 3 {
		t.Fatalf("reloaded %d characters, want 3", len(reloaded))
	}
	byName := map[string]*enterworld.Character{}
	for _, c := range reloaded {
		byName[strings.ToLower(c.Name)] = c
	}

	for _, original := range []*enterworld.Character{neverSeeded, seeded, seededEmpty} {
		loaded, ok := byName[strings.ToLower(original.Name)]
		if !ok {
			t.Fatalf("character %s lost across restart", original.Name)
		}
		want, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(loaded)
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(got) {
			t.Fatalf("%s roundtrip mismatch:\nwant %s\ngot  %s", original.Name, want, got)
		}
	}

	// nil-vs-empty is load-bearing: never-seeded must come back nil (the
	// file carries null), seeded-empty must come back non-nil empty.
	if byName["never_seeded"].MissionInventory != nil {
		t.Fatal("never-seeded inventory must reload as nil (null in the file); a [] here would suppress the first-boot starter seed")
	}
	if byName["emptybagfix"].MissionInventory == nil {
		t.Fatal("seeded-empty inventory must reload as non-nil []; a nil here would re-run the starter seed")
	}

	// The canary rides as an opaque string: byte-exact after reload.
	rows := byName["asd2fixture"].MissionInventory
	if len(rows) != 1 || rows[0].VarianceBits != varianceCanary {
		t.Fatalf("varianceBits canary corrupted: %+v", rows)
	}

	// And the persisted record itself spells null for never-seeded (raw
	// witness, straight off the database row).
	record := readCharacterRecordRaw(t, s2, testDivision, "never_seeded")
	if !strings.Contains(record, `"missionInventory":null`) {
		t.Fatalf("persisted record must carry missionInventory:null for a never-seeded character, got %s", record)
	}
}

// TestMissingStorePolicy pins boot semantics: empty first
// boot only when NOTHING says a store should exist; every other absence
// refuses.
func TestMissingStorePolicy(t *testing.T) {
	t.Parallel()
	clock := newTestClock()

	t.Run("fresh dir boots empty without Require", func(t *testing.T) {
		dir := t.TempDir()
		s := openTest(t, dir, clock)
		if got := len(s.Characters().CharactersForDivision(testDivision)); got != 0 {
			t.Fatalf("fresh store has %d characters", got)
		}
		if _, err := os.Stat(filepath.Join(dir, DBFileName)); err != nil {
			t.Fatalf("fresh development boot did not atomically publish its empty database: %v", err)
		}
	})

	t.Run("Require refuses a missing store", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := Open(dir, Options{Now: clock.Now, RequireStore: true}); err == nil {
			t.Fatal("RequireStore with no store must refuse")
		}
	})

	t.Run("bak without main recovers instead of fabricating empty", func(t *testing.T) {
		dir := t.TempDir()
		seed := openTest(t, dir, clock)
		if err := seed.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: "survivor"}); err != nil {
			t.Fatal(err)
		}
		seed.Close()
		// A reopen refreshes the bak; then simulate the main database lost.
		openTest(t, dir, clock).Close()
		if err := os.Remove(filepath.Join(dir, DBFileName)); err != nil {
			t.Fatal(err)
		}
		removeDBSidecars(filepath.Join(dir, DBFileName))
		s := openTest(t, dir, clock)
		if len(s.Characters().CharactersForDivision(testDivision)) != 1 {
			t.Fatal("bak-only boot must recover the previous generation, not boot empty")
		}
		if !s.Health().LoadedFromBak {
			t.Fatal("bak recovery must flag Health.LoadedFromBak")
		}
		if _, err := os.Stat(filepath.Join(dir, DBFileName)); err != nil {
			t.Fatal("bak recovery must restore the main database immediately")
		}
	})
}

// TestQuarantineBakLadder: corruption quarantines the main file (bytes
// preserved), the previous generation loads LOUD-YELLOW, and the
// retention contract caps quarantine debris at 3.
func TestQuarantineBakLadder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s1 := openTest(t, dir, clock)
	if err := s1.CreateCharacter(testDivision, "test-account", seededCharacter()); err != nil {
		t.Fatal(err)
	}
	s1.Close()
	openTest(t, dir, clock).Close() // refreshes bak from the good main

	dbPath := filepath.Join(dir, DBFileName)
	if err := os.WriteFile(dbPath, []byte(`TORN - not a database`), 0o644); err != nil {
		t.Fatal(err)
	}
	removeDBSidecars(dbPath)

	clock.Advance(time.Second)
	s3 := openTest(t, dir, clock)
	health := s3.Health()
	if !health.LoadedFromBak {
		t.Fatal("corrupt main with good bak must recover from bak (loud yellow)")
	}
	if got := len(s3.Characters().CharactersForDivision(testDivision)); got != 1 {
		t.Fatalf("recovered store has %d characters, want 1", got)
	}
	quarantines, err := filepath.Glob(dbPath + corruptSuffix + "*")
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("exactly one quarantine artifact expected, got %v (err %v)", quarantines, err)
	}
	preserved, err := os.ReadFile(quarantines[0])
	if err != nil || !strings.Contains(string(preserved), "TORN") {
		t.Fatalf("quarantine must preserve the corrupt bytes for forensics, got %q (err %v)", preserved, err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal("bak recovery must restore the main database immediately")
	}
	s3.Close()

	// Retention: three more corruption cycles -> newest 3 kept, oldest gone.
	for i := 0; i < 3; i++ {
		clock.Advance(time.Second)
		if err := os.WriteFile(dbPath, []byte(fmt.Sprintf(`TORN-%d not a database`, i)), 0o644); err != nil {
			t.Fatal(err)
		}
		removeDBSidecars(dbPath)
		recovered := openTest(t, dir, clock)
		recovered.Close()
	}
	quarantines, _ = filepath.Glob(dbPath + corruptSuffix + "*")
	if len(quarantines) != keepQuarantines {
		t.Fatalf("retention contract violated: %d quarantines on disk, want %d", len(quarantines), keepQuarantines)
	}
}

// TestBothGenerationsCorruptRefuses: when main AND bak are unusable the
// store refuses to serve rather than fabricate a fresh level-1 world.
func TestBothGenerationsCorruptRefuses(t *testing.T) {
	t.Parallel()
	clock := newTestClock()

	t.Run("database family", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, DBFileName), []byte("garbage-main"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, DBBakFileName), []byte("garbage-bak"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(dir, Options{Now: clock.Now}); err == nil {
			t.Fatal("corrupt database + corrupt bak must refuse, never boot empty")
		}
	})

}

// TestFutureVersionRefusesInPlace: a store from a NEWER binary refuses
// WITHOUT quarantining the file and WITHOUT falling back to the older
// bak generation (that would be a silent rollback).
func TestFutureVersionRefusesInPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s1 := openTest(t, dir, clock)
	if err := s1.CreateCharacter(testDivision, "test-account", seededCharacter()); err != nil {
		t.Fatal(err)
	}
	s1.Close()
	openTest(t, dir, clock).Close() // bak now holds a perfectly loadable OLD generation

	// Stamp the database as a FUTURE schema version (a newer binary's
	// store).
	dbPath := filepath.Join(dir, DBFileName)
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", metaKeySchemaVersion, fmt.Sprintf("%d", CurrentVersion+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err = Open(dir, Options{Now: clock.Now})
	if err == nil {
		t.Fatal("future-version store must refuse")
	}
	if !isVersionMismatch(errors.Unwrap(err)) && !isVersionMismatch(err) {
		t.Fatalf("future-version refusal must classify as version mismatch, got: %v", err)
	}
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Fatal("future-version database must stay in place")
	}
	if quarantines, _ := filepath.Glob(dbPath + corruptSuffix + "*"); len(quarantines) != 0 {
		t.Fatalf("future-version database must not be quarantined, found %v", quarantines)
	}
	// And it must NOT have fallen back to the older bak generation (that
	// would be a silent rollback): the refusal is the assertion above.
}

// TestCounterWatermarks: nextCharId allocates-then-increments per
// division, persists, and never reuses; the gid counter rides meta and
// the registry snapshot.
func TestCounterWatermarks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s1 := openTest(t, dir, clock)
	first := &enterworld.Character{Name: "first"}
	second := &enterworld.Character{Name: "second"}
	if err := s1.CreateCharacter(testDivision, "test-account", first); err != nil {
		t.Fatal(err)
	}
	if err := s1.CreateCharacter(testDivision, "test-account", second); err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("fresh division ids = %d, %d; want 1, 2", first.ID, second.ID)
	}
	if err := s1.CreateCharacter(testDivision, "test-account", &enterworld.Character{Name: "FIRST"}); err == nil {
		t.Fatal("case-insensitive name conflict must refuse")
	}
	s1.Close() // a real restart closes the first instance (single-writer guard)

	s2 := openTest(t, dir, clock)
	if got := s2.MetaView().NextCharID[testDivision]; got != 3 {
		t.Fatalf("persisted nextCharId = %d, want 3", got)
	}
	third := &enterworld.Character{Name: "third"}
	if err := s2.CreateCharacter(testDivision, "test-account", third); err != nil {
		t.Fatal(err)
	}
	if third.ID != 3 {
		t.Fatalf("post-restart id = %d, want 3 (allocate-then-increment)", third.ID)
	}

}

// TestGroundTTLContinuityThroughStore: DroppedAt persists as the ORIGINAL
// wall-clock instant, never-expires rows stay never-expires across
// reboots, expiry keys off the restored timestamp, and the gid counter
// continues (superseding the persist.go file-half's coverage).
func TestGroundTTLContinuityThroughStore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	dropInstant := clock.Now().Add(-100 * time.Second) // mid-TTL at boot

	s := openTest(t, dir, clock)
	registry := grounditem.NewRegistry()
	s.AttachGround(registry)
	timed := registry.Add(testDivision, grounditem.Item{RefObjID: 3117, Codename: "ITEM_ETC_GOLD", TypeFlags: 0x00C0, Position: grounditem.Point{RegionID: 0x6b4f, X: 1, Z: 3}, Y: 2, DroppedAt: dropInstant, DroppedBy: "asd2"})
	registry.Add(testDivision, grounditem.Item{RefObjID: 3118, Codename: "ITEM_NEVER_EXPIRES", TypeFlags: 0x00C0, Position: grounditem.Point{RegionID: 0x6b4f, X: 4, Z: 6}, Y: 5})
	s.Mutate("seed-ground", nil)
	s.Close()

	s = openTest(t, dir, clock)
	registry = grounditem.NewRegistry()
	registry.Restore(s.GroundSnapshotForRestore())
	s.AttachGround(registry)

	items := registry.All(testDivision)
	if len(items) != 2 {
		t.Fatalf("restored %d ground items, want 2", len(items))
	}
	if got := items[0].DroppedAt.UnixMilli(); got != dropInstant.UnixMilli() {
		t.Fatalf("DroppedAt restamped: got %d, want the ORIGINAL %d (TTL must continue across reboots)", got, dropInstant.UnixMilli())
	}
	if !items[1].DroppedAt.IsZero() {
		t.Fatal("never-expires row gained a timestamp on restore")
	}

	// The wall-clock deadline: 100s elapsed pre-kill + 81s post-boot
	// crosses the 180s fixture lifetime; the timed row expires, the
	// never-expires row does not.
	expireAt := clock.Now().Add(81 * time.Second)
	expired := registry.ExpireItems(testDivision, expireAt, grounditem.FixtureLifetime)
	if len(expired) != 1 || expired[0].Gid != timed.Gid {
		t.Fatalf("expiry across reboot wrong: %+v", expired)
	}

	// Gid continuity: the next drop must take a FRESH gid past the
	// restored watermark, and the commit must persist both facts.
	added := registry.Add(testDivision, grounditem.Item{RefObjID: 3119, Codename: "ITEM_FRESH_DROP", TypeFlags: 0x00C0, DroppedAt: expireAt})
	if added.Gid != grounditem.GidBase+3 {
		t.Fatalf("post-restore gid = %d, want %d (counter continuity)", added.Gid, grounditem.GidBase+3)
	}
	s.Mutate("ttl-sweep", nil)
	s.Close() // a real restart closes the first instance (single-writer guard)

	s2 := openTest(t, dir, clock)
	snapshot := s2.GroundSnapshotForRestore()
	if snapshot.GidCounter != 3 {
		t.Fatalf("persisted gidCounter = %d, want 3", snapshot.GidCounter)
	}
	rows := snapshot.Divisions[testDivision]
	if len(rows) != 2 {
		t.Fatalf("persisted %d ground rows, want 2 (expired row gone, never-expires + fresh kept)", len(rows))
	}
	for _, row := range rows {
		if row.Codename == "ITEM_NEVER_EXPIRES" && row.DroppedAtMs != 0 {
			t.Fatal("never-expires row must persist without a timestamp after N reboots")
		}
	}
}

// TestHealthDegradesAndRecovers: runtime write failure is fail-open and
// LOUD - the mutation stands in memory, Health degrades with the op
// label, and the next successful commit self-heals.
func TestHealthDegradesAndRecovers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	if err := s.CreateCharacter(testDivision, "test-account", seededCharacter()); err != nil {
		t.Fatal(err)
	}

	record := s.Characters().CharactersForDivision(testDivision)[0]

	s.commitFail = errors.New("disk on fire")
	s.Mutate("inv-move asd2fixture", func() {
		record.Gold = int64Ptr(99999)
	})

	health := s.Health()
	if health.FailedWrites != 1 || !strings.Contains(health.LastError, "disk on fire") {
		t.Fatalf("health must record the failure: %+v", health)
	}
	if !strings.Contains(health.LastError, "inv-move") {
		t.Fatalf("health must carry the op label for attribution: %+v", health)
	}
	if *record.Gold != 99999 {
		t.Fatal("fail-open: the in-memory mutation must stand")
	}

	s.commitFail = nil
	s.Mutate("inv-move asd2fixture", nil)
	health = s.Health()
	if health.FailedWrites != 0 || health.LastError != "" {
		t.Fatalf("health must self-heal after a successful commit: %+v", health)
	}

	// The healed commit carries the mutation made during the outage.
	s.Close()
	s2 := openTest(t, dir, clock)
	reloaded := s2.Characters().CharactersForDivision(testDivision)[0]
	if reloaded.Gold == nil || *reloaded.Gold != 99999 {
		t.Fatal("the outage-window mutation must reach disk with the healing commit")
	}
}

// TestBakRefreshOncePerBoot: exactly one bak generation, refreshed at
// load time only - never advanced by running commits.
func TestBakRefreshOncePerBoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	bakPath := filepath.Join(dir, DBBakFileName)

	s1 := openTest(t, dir, clock)
	if err := s1.CreateCharacter(testDivision, "test-account", seededCharacter()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bakPath); !os.IsNotExist(err) {
		t.Fatal("first boot has no previous generation - no bak until a later boot proves the main database")
	}
	s1.Close()

	s2 := openTest(t, dir, clock)
	bakBytes, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("bak must refresh from the proven main at boot (err %v)", err)
	}

	record := s2.Characters().CharactersForDivision(testDivision)[0]
	clock.Advance(time.Second)
	s2.Mutate("move", func() { record.Gold = int64Ptr(777) })

	bakAfter, _ := os.ReadFile(bakPath)
	if string(bakAfter) != string(bakBytes) {
		t.Fatal("a running commit must NOT advance the bak generation")
	}
	s2.Close()

	// The bak is the PREVIOUS generation: restored into a fresh dir it
	// must load, and it must NOT carry the post-refresh commit.
	bakDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bakDir, DBFileName), bakBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	fromBak := openTest(t, bakDir, clock)
	previous := fromBak.Characters().CharactersForDivision(testDivision)[0]
	if previous.Gold != nil && *previous.Gold == 777 {
		t.Fatal("the bak generation must predate the running commit")
	}

	// And the commit itself reached the MAIN generation.
	s3 := openTest(t, dir, clock)
	current := s3.Characters().CharactersForDivision(testDivision)[0]
	if current.Gold == nil || *current.Gold != 777 {
		t.Fatal("the running commit must be durable in the main database")
	}
}

// TestConcurrentMutateSerializes: the door is the only writer and
// serializes commits; concurrent readers stay race-free (-race gate).
func TestConcurrentMutateSerializes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	record := seededCharacter()
	if err := s.CreateCharacter(testDivision, "test-account", record); err != nil {
		t.Fatal(err)
	}

	gold := int64(0)
	record.Gold = &gold

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				s.Mutate("pickup-gold", func() { *record.Gold++ })
			}
		}()
	}
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				s.Characters().CharactersForDivision(testDivision)
				s.Health()
			}
		}()
	}
	wg.Wait()

	if *record.Gold != 200 {
		t.Fatalf("door lost mutations: gold = %d, want 200", *record.Gold)
	}
	s.Close() // a real restart closes the first instance (single-writer guard)
	s2 := openTest(t, dir, clock)
	reloaded := s2.Characters().CharactersForDivision(testDivision)[0]
	if reloaded.Gold == nil || *reloaded.Gold != 200 {
		t.Fatalf("persisted gold = %v, want 200", reloaded.Gold)
	}
}

// TestReapMaturedDeletions: the deletion reaper archives a reservation
// only after the 7-day window, byte-preserves the record in the
// deletedCharacters archive across restarts, never reuses the id, is
// fail-closed under a write outage, and never trusts a garbage timestamp.
