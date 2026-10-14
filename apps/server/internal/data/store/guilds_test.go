package store

// Guild-plane (guilds + guild_members tables) persistence and authority
// graph tests.

import (
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

func guildTestCharacter(name string) *enterworld.Character {
	return &enterworld.Character{
		Name:          name,
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     int64Ptr(enterworld.RaceChina),
		Gender:        int64Ptr(enterworld.GenderMale),
	}
}

func addTestGuildMember(
	s *Store,
	guildID int64,
	actorID int64,
	member enterworld.GuildMemberRecord,
) bool {
	_, refusal := s.Guilds().AddGuildMemberAs(
		testDivision,
		guildID,
		actorID,
		0,
		member,
	)
	return !refusal.Refused()
}

func updateTestGuild(
	s *Store,
	actorID int64,
	label string,
	update func(
		enterworld.GuildRecord,
		[]enterworld.GuildMemberRecord,
	) (enterworld.GuildRecord, []enterworld.GuildMemberRecord),
) bool {
	_, refusal := s.Guilds().UpdateGuildAs(
		testDivision,
		actorID,
		label,
		enterworld.GuildAuthorization{},
		func(
			guild enterworld.GuildRecord,
			members []enterworld.GuildMemberRecord,
		) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
			nextGuild, nextMembers := update(guild, members)
			return nextGuild, nextMembers, true
		},
	)
	return !refusal.Refused()
}

// seedTestGuild installs one guild with the two characters as members
// through the door (the honest Phase C state source - no create opcode
// exists in this lane).
func seedTestGuild(t *testing.T, s *Store, leader, member *enterworld.Character) (int64, enterworld.GuildRecord, []enterworld.GuildMemberRecord) {
	t.Helper()
	leaderLevel := int64(10)
	memberLevel := int64(4)
	if !s.UpdateCharacters(
		[]*enterworld.Character{leader, member},
		"guild-fixture-levels",
		func() bool {
			leader.Level = &leaderLevel
			member.Level = &memberLevel
			return true
		},
	) {
		t.Fatal("fixture character levels refused")
	}
	guild := enterworld.GuildRecord{
		Name:           "NightWatch",
		Level:          2,
		GP:             1500,
		NoticeSubject:  "notice subject",
		NoticeContents: "notice contents",
		CrestParam:     0x00c81234,
		Byte10:         0,
	}
	members := []enterworld.GuildMemberRecord{
		{CharID: leader.ID, JID: uint32(100000 + leader.ID), Name: leader.Name, Grade: 0, Level: 10, DonatedGP: 900, PermMask: 0xffffffff, GrantName: "Leader", RefObjID: 1907},
		{CharID: member.ID, JID: uint32(100000 + member.ID), Name: member.Name, Grade: 3, Level: 4, DonatedGP: 600, PermMask: 0, GrantName: "", RefObjID: 1907},
	}
	guildID, err := s.Guilds().CreateGuild(testDivision, guild, members[0], leader)
	if err != nil {
		t.Fatal(err)
	}
	if !addTestGuildMember(s, guildID, leader.ID, members[1]) {
		t.Fatal("fixture member join refused")
	}
	guild.ID = guildID
	// The store stamps the founding time from its own clock.
	guild.FoundedAtMs = s.now().UnixMilli()
	return guildID, guild, members
}

func TestGuildMemberLevelIsCharacterOwnedProjection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s := openTest(t, dir, clock)
	leader := guildTestCharacter("levelowner")
	member := guildTestCharacter("levelmember")
	for _, character := range []*enterworld.Character{leader, member} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}
	guildID, _, _ := seedTestGuild(t, s, leader, member)

	var persisted string
	if err := s.db.QueryRow(
		"SELECT record FROM guild_members WHERE division = ? AND guild_id = ? AND char_id = ?",
		testDivision,
		guildID,
		member.ID,
	).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, `"level"`) {
		t.Fatalf("guild membership persists character-owned level: %s", persisted)
	}

	raised := int64(37)
	if !s.UpdateCharacter(member, "level-owner-raise", func() bool {
		member.Level = &raised
		return true
	}) {
		t.Fatal("character level update refused")
	}
	_, members, ok := s.Guilds().Guild(testDivision, guildID)
	if !ok || len(members) != 2 || members[1].Level != uint8(raised) {
		t.Fatalf("guild view after level-up = %+v/%v, want member level %d", members, ok, raised)
	}

	s.Close()
	reopened := openTest(t, dir, clock)
	_, restored, ok := reopened.Guilds().Guild(testDivision, guildID)
	if !ok || len(restored) != 2 || restored[1].Level != uint8(raised) {
		t.Fatalf("guild view after reopen = %+v/%v, want member level %d", restored, ok, raised)
	}
}

// TestDonateGuildPointsAtomicDoor pins the GP-donate door (the 0x740F
// job): ONE commit moves the SP debit, the guild GP credit and the
// member DonatedGP credit together, all three survive a reopen, and
// every refusal arm (unknown guild, non-member, unknown character,
// zero amount, SP deficit, u32 wrap) mutates nothing.
func TestDonateGuildPointsAtomicDoor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s := openTest(t, dir, clock)
	leader := guildTestCharacter("gpdonlead")
	member := guildTestCharacter("gpdonmate")
	if err := s.CreateCharacter(testDivision, "test-account", leader); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(testDivision, "test-account", member); err != nil {
		t.Fatal(err)
	}
	guildID, _, _ := seedTestGuild(t, s, leader, member)
	s.MutateCharacter(member, "gp-donate-seed-sp", func() { member.SkillPoints = int64Ptr(500) })

	donation, refusal := s.Guilds().DonateGuildPoints(testDivision, member.ID, 120)
	if refusal.Refused() {
		t.Fatal("donate door refused a valid donation")
	}
	if donation.Snapshot.Guild.GP != 1620 {
		t.Errorf("new guild GP = %d, want 1500+120", donation.Snapshot.Guild.GP)
	}
	if donation.Donor.DonatedGP != 720 {
		t.Errorf("new donated GP = %d, want 600+120", donation.Donor.DonatedGP)
	}
	if member.SkillPoints == nil || *member.SkillPoints != 380 {
		t.Errorf("donor SP = %v, want 380", member.SkillPoints)
	}

	// Refusal arms leave all three planes
	// untouched.
	refusals := []struct {
		name   string
		charID int64
		amount uint32
	}{
		{"non-member character", leader.ID + member.ID + 100, 10},
		{"zero amount", member.ID, 0},
		{"SP deficit", member.ID, 381},
		{"u32 wrap", member.ID, 0xffffffff},
	}
	for _, tc := range refusals {
		if _, refusal := s.Guilds().DonateGuildPoints(testDivision, tc.charID, tc.amount); !refusal.Refused() {
			t.Errorf("%s: door accepted", tc.name)
		}
	}
	guild, members, _ := s.Guilds().Guild(testDivision, guildID)
	if guild.GP != 1620 {
		t.Errorf("guild GP after refusals = %d, want 1620", guild.GP)
	}
	if members[1].DonatedGP != 720 {
		t.Errorf("member donated GP after refusals = %d, want 720", members[1].DonatedGP)
	}
	if *member.SkillPoints != 380 {
		t.Errorf("donor SP after refusals = %d, want 380", *member.SkillPoints)
	}

	// The ONE commit persisted all three sides: reopen and re-read.
	s.Close()
	reopened := openTest(t, dir, clock)
	guild2, members2, ok := reopened.Guilds().Guild(testDivision, guildID)
	if !ok {
		t.Fatal("guild lost across reopen")
	}
	if guild2.GP != 1620 {
		t.Errorf("reopened guild GP = %d, want 1620", guild2.GP)
	}
	if members2[1].DonatedGP != 720 {
		t.Errorf("reopened donated GP = %d, want 720", members2[1].DonatedGP)
	}
	var member2 *enterworld.Character
	for _, c := range reopened.Characters().CharactersForDivision(testDivision) {
		if c.Name == "gpdonmate" {
			member2 = c
			break
		}
	}
	if member2 == nil {
		t.Fatal("donor lost across reopen")
	}
	if member2.SkillPoints == nil || *member2.SkillPoints != 380 {
		t.Errorf("reopened donor SP = %v, want 380", member2.SkillPoints)
	}
}

func TestGuildPersistsAcrossReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s := openTest(t, dir, clock)
	leader := guildTestCharacter("guildlead")
	member := guildTestCharacter("guildmate")
	if err := s.CreateCharacter(testDivision, "test-account", leader); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(testDivision, "test-account", member); err != nil {
		t.Fatal(err)
	}
	guildID, wantGuild, wantMembers := seedTestGuild(t, s, leader, member)

	// The Guild read is a copy: mutating it must not leak into the live
	// plane.
	_, leak, ok := s.Guilds().Guild(testDivision, guildID)
	if !ok {
		t.Fatal("seeded guild not readable")
	}
	leak[0].DonatedGP = 1
	if _, got, _ := s.Guilds().Guild(testDivision, guildID); got[0].DonatedGP != wantMembers[0].DonatedGP {
		t.Fatal("Guild returned the live backing array")
	}
	if gid, ok := s.Guilds().GuildOfCharacter(testDivision, member.ID); !ok || gid != guildID {
		t.Fatalf("GuildOfCharacter = %d/%v, want %d/true", gid, ok, guildID)
	}

	s.Close()
	s2 := openTest(t, dir, clock)
	restoredGuild, restoredMembers, ok := s2.Guilds().Guild(testDivision, guildID)
	if !ok || restoredGuild != wantGuild {
		t.Fatalf("restored guild = %+v/%v, want %+v", restoredGuild, ok, wantGuild)
	}
	if len(restoredMembers) != 2 || restoredMembers[0] != wantMembers[0] || restoredMembers[1] != wantMembers[1] {
		t.Fatalf("restored members = %+v, want %+v", restoredMembers, wantMembers)
	}

	// The atomic ADD door (the invite handshake's commit): a third
	// character joins - the member row appends AND the GuildID FK sets
	// in the same commit; every refusal arm mutates nothing.
	joiner := guildTestCharacter("guildjoin")
	if err := s2.CreateCharacter(testDivision, "test-account", joiner); err != nil {
		t.Fatal(err)
	}
	joinRow := enterworld.GuildMemberRecord{CharID: joiner.ID, JID: uint32(100000 + joiner.ID), Name: joiner.Name, Grade: 0x0a, Level: 1, RefObjID: 1907}
	if addTestGuildMember(s2, 999, leader.ID, joinRow) {
		t.Fatal("AddGuildMemberAs committed into a guild that does not exist")
	}
	if addTestGuildMember(s2, guildID, leader.ID, enterworld.GuildMemberRecord{CharID: 424242, Name: "ghost"}) {
		t.Fatal("AddGuildMemberAs committed an unknown character record")
	}
	if !addTestGuildMember(s2, guildID, leader.ID, joinRow) {
		t.Fatal("AddGuildMemberAs refused a valid join")
	}
	if joiner.GuildID == nil || *joiner.GuildID != guildID {
		t.Fatalf("joiner FK = %v, want %d (set in the SAME commit)", joiner.GuildID, guildID)
	}
	if addTestGuildMember(s2, guildID, leader.ID, joinRow) {
		t.Fatal("AddGuildMemberAs committed a duplicate member row")
	}
	riddenLeader := enterworld.GuildMemberRecord{CharID: leader.ID, JID: 1, Name: leader.Name}
	if addTestGuildMember(s2, guildID, leader.ID, riddenLeader) {
		t.Fatal("AddGuildMemberAs committed a character whose FK already points at a guild")
	}
	if _, joinedMembers, _ := s2.Guilds().Guild(testDivision, guildID); len(joinedMembers) != 3 || joinedMembers[2] != joinRow {
		t.Fatalf("members after the join = %+v, want the appended row %+v last", joinedMembers, joinRow)
	}

	// Mutate the notice and one member's donation; the next reopen must
	// carry exactly the swapped state (whole-guild replace on commit).
	updateTestGuild(s2, leader.ID, "guild-notice-edit", func(guild enterworld.GuildRecord, members []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord) {
		guild.NoticeSubject = "edited subject"
		members[1].DonatedGP = 750
		return guild, members
	})
	s2.Close()

	s3 := openTest(t, dir, clock)
	finalGuild, finalMembers, ok := s3.Guilds().Guild(testDivision, guildID)
	if !ok || finalGuild.NoticeSubject != "edited subject" {
		t.Fatalf("final guild = %+v/%v, want the edited subject", finalGuild, ok)
	}
	if len(finalMembers) != 3 || finalMembers[1].DonatedGP != 750 || finalMembers[2] != joinRow {
		t.Fatalf("final members = %+v, want member[1] donatedGp 750 and the joined row %+v last", finalMembers, joinRow)
	}
	if gid, ok := s3.Guilds().GuildOfCharacter(testDivision, joiner.ID); !ok || gid != guildID {
		t.Fatalf("reopened GuildOfCharacter(joiner) = %d/%v, want %d/true (the ADD door's row persisted)", gid, ok, guildID)
	}
}

func TestGuildTopologyOnlyMovesThroughDedicatedDoors(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	leader := guildTestCharacter("topolead")
	member := guildTestCharacter("topomate")
	outsider := guildTestCharacter("topooutside")
	for _, character := range []*enterworld.Character{leader, member, outsider} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}
	guildID, _, _ := seedTestGuild(t, s, leader, member)

	if updateTestGuild(s, outsider.ID, "forbidden-create", func(enterworld.GuildRecord, []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord) {
		return enterworld.GuildRecord{ID: guildID + 99, Name: "Bypass"}, nil
	}) {
		t.Fatal("UpdateGuildAs accepted a guildless actor")
	}
	if updateTestGuild(s, leader.ID, "forbidden-join", func(guild enterworld.GuildRecord, members []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord) {
		return guild, append(members, enterworld.GuildMemberRecord{CharID: outsider.ID, Name: outsider.Name, Grade: 3})
	}) {
		t.Fatal("UpdateGuildAs changed roster topology")
	}
	if updateTestGuild(s, leader.ID, "forbidden-leader-loss", func(guild enterworld.GuildRecord, members []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord) {
		members[0].Grade = 1
		return guild, members
	}) {
		t.Fatal("UpdateGuildAs created a leaderless guild")
	}
	if addTestGuildMember(s, guildID, leader.ID, enterworld.GuildMemberRecord{CharID: outsider.ID, Name: outsider.Name, Grade: 0}) {
		t.Fatal("AddGuildMemberAs added a second leader")
	}
	if _, refusal := s.Guilds().LeaveGuild(testDivision, leader.ID); !refusal.Refused() {
		t.Fatal("LeaveGuild removed the leader instead of requiring dissolution")
	}
	if _, members, ok := s.Guilds().Guild(testDivision, guildID); !ok || len(members) != 2 || members[0].Grade != 0 {
		t.Fatalf("guild changed after topology refusals: %+v ok=%v", members, ok)
	}
	if outsider.GuildID != nil {
		t.Fatalf("refused topology mutation set outsider FK to %v", *outsider.GuildID)
	}
}

func TestLoadRefusesDeletePendingGuildMember(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	leader := guildTestCharacter("loadlead")
	member := guildTestCharacter("loadmate")
	for _, character := range []*enterworld.Character{leader, member} {
		if err := s.CreateCharacter(testDivision, "test-account", character); err != nil {
			t.Fatal(err)
		}
	}
	seedTestGuild(t, s, leader, member)
	s.MutateCharacter(member, "inject-invalid-social-state", func() {
		member.DeletePending = true
		member.DeleteReservedAt = time.Now().UTC().Format(time.RFC3339)
	})

	if _, err := loadDB(s.db, CurrentVersion, CurrentLayoutVersion); err == nil ||
		!strings.Contains(err.Error(), "delete-pending character") {
		t.Fatalf("loadDB malformed social graph error = %v", err)
	}
}

// TestGuildIDWatermarkPersistsAndNeverReissues proves the next_guild_id
// watermark is next_char_id's honest twin: ids allocate monotonically,
// the watermark row survives a reopen, and a rebooted store continues
// PAST every id it ever issued instead of re-counting live rows.
func TestGuildIDWatermarkPersistsAndNeverReissues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s := openTest(t, dir, clock)
	leaders := make([]*enterworld.Character, 3)
	for i, name := range []string{"gwmlead1", "gwmlead2", "gwmlead3"} {
		leaders[i] = guildTestCharacter(name)
		if err := s.CreateCharacter(testDivision, "test-account", leaders[i]); err != nil {
			t.Fatal(err)
		}
	}
	newGuild := func(name string, leader *enterworld.Character) (enterworld.GuildRecord, enterworld.GuildMemberRecord) {
		return enterworld.GuildRecord{Name: name, Level: 1},
			enterworld.GuildMemberRecord{CharID: leader.ID, JID: uint32(leader.ID), Name: leader.Name, PermMask: 0xffffffff}
	}

	guild1, member1 := newGuild("WatermarkOne", leaders[0])
	id1, err := s.Guilds().CreateGuild(testDivision, guild1, member1, leaders[0])
	if err != nil {
		t.Fatal(err)
	}
	guild2, member2 := newGuild("WatermarkTwo", leaders[1])
	id2, err := s.Guilds().CreateGuild(testDivision, guild2, member2, leaders[1])
	if err != nil {
		t.Fatal(err)
	}
	if id1 != 1 || id2 != 2 {
		t.Fatalf("allocated ids = %d, %d, want 1, 2", id1, id2)
	}
	s.Close()

	s2 := openTest(t, dir, clock)
	guild3, member3 := newGuild("Watermark3", leaders[2])
	leader3 := func() *enterworld.Character {
		for _, c := range s2.Characters().CharactersForDivision(testDivision) {
			if c.Name == "gwmlead3" {
				return c
			}
		}
		t.Fatal("gwmlead3 not reloaded")
		return nil
	}()
	member3.CharID = leader3.ID
	id3, err := s2.Guilds().CreateGuild(testDivision, guild3, member3, leader3)
	if err != nil {
		t.Fatal(err)
	}
	if id3 != 3 {
		t.Fatalf("post-reboot allocation = %d, want 3 (the watermark must persist, never re-count)", id3)
	}
}

// TestCreateGuildAtomicity proves the combined door's whole point: after
// a reopen the guild row, the leader's member row AND the leader
// character's GuildID FK are ALL present - one commit, never a torn
// guild-vs-FK state. The door's validation refusals (case-insensitive
// name conflict) mutate nothing.
func TestCreateGuildAtomicity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s := openTest(t, dir, clock)
	leader := guildTestCharacter("gatomlead")
	other := guildTestCharacter("gatomother")
	if err := s.CreateCharacter(testDivision, "test-account", leader); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(testDivision, "test-account", other); err != nil {
		t.Fatal(err)
	}
	record := enterworld.GuildRecord{Name: "AtomWatch", Level: 1, GP: 0}
	member := enterworld.GuildMemberRecord{CharID: leader.ID, JID: uint32(leader.ID), Name: leader.Name, Grade: 0, Level: 1, PermMask: 0xffffffff, RefObjID: 1907}
	id, err := s.Guilds().CreateGuild(testDivision, record, member, leader)
	if err != nil {
		t.Fatal(err)
	}
	if leader.GuildID == nil || *leader.GuildID != id {
		t.Fatalf("leader FK = %v, want %d", leader.GuildID, id)
	}

	// The case-insensitive conflict refuses BEFORE any mutation (the
	// CreateCharacter EqualFold precedent) and leaves the other
	// character guildless.
	if _, err := s.Guilds().CreateGuild(testDivision, enterworld.GuildRecord{Name: "atomwatch"},
		enterworld.GuildMemberRecord{CharID: other.ID, Name: other.Name}, other); err == nil {
		t.Fatal("case-insensitive duplicate guild name accepted")
	}
	if other.GuildID != nil {
		t.Fatalf("refused creation set the FK to %v", *other.GuildID)
	}
	s.Close()

	s2 := openTest(t, dir, clock)
	restoredGuild, restoredMembers, ok := s2.Guilds().Guild(testDivision, id)
	if !ok || restoredGuild.Name != "AtomWatch" || restoredGuild.ID != id {
		t.Fatalf("reopened guild = %+v/%v, want AtomWatch id %d", restoredGuild, ok, id)
	}
	if len(restoredMembers) != 1 || restoredMembers[0] != member {
		t.Fatalf("reopened members = %+v, want the leader row %+v", restoredMembers, member)
	}
	for _, c := range s2.Characters().CharactersForDivision(testDivision) {
		if c.Name != leader.Name {
			continue
		}
		if c.GuildID == nil || *c.GuildID != id {
			t.Fatalf("reopened leader FK = %v, want %d (the commit must carry all three pieces)", c.GuildID, id)
		}
	}
}

// TestRemoveGuildMemberClearsRowAndFK proves the removal door's atomic
// pair: the member row is gone AND that character's GuildID FK is
// cleared - immediately and across a reopen - while the guild row and
// every other member stay intact. A miss (no such member) mutates
// nothing and returns false.
func TestRemoveGuildMemberClearsRowAndFK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s := openTest(t, dir, clock)
	leader := guildTestCharacter("gremlead")
	member := guildTestCharacter("gremmate")
	if err := s.CreateCharacter(testDivision, "test-account", leader); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(testDivision, "test-account", member); err != nil {
		t.Fatal(err)
	}
	id, err := s.Guilds().CreateGuild(testDivision, enterworld.GuildRecord{Name: "RemoveWatch"},
		enterworld.GuildMemberRecord{CharID: leader.ID, JID: uint32(leader.ID), Name: leader.Name, PermMask: 0xffffffff}, leader)
	if err != nil {
		t.Fatal(err)
	}
	if !addTestGuildMember(s, id, leader.ID, enterworld.GuildMemberRecord{CharID: member.ID, JID: uint32(member.ID), Name: member.Name, Grade: 3}) {
		t.Fatal("fixture join refused")
	}

	if _, refusal := s.Guilds().KickGuildMember(testDivision, leader.ID, "not-a-member", 1); !refusal.Refused() {
		t.Fatal("removal of a non-member reported success")
	}
	if _, refusal := s.Guilds().KickGuildMember(testDivision, leader.ID, member.Name, 1); refusal.Refused() {
		t.Fatal("removal of a live member reported failure")
	}
	if member.GuildID != nil {
		t.Fatalf("removed member FK = %v, want nil", *member.GuildID)
	}
	if leader.GuildID == nil || *leader.GuildID != id {
		t.Fatal("the leader's FK moved - the door must touch only the removed member")
	}
	s.Close()

	s2 := openTest(t, dir, clock)
	reopenedGuild, reopenedMembers, ok := s2.Guilds().Guild(testDivision, id)
	if !ok || reopenedGuild.Name != "RemoveWatch" {
		t.Fatalf("reopened guild = %+v/%v, want the intact row", reopenedGuild, ok)
	}
	if len(reopenedMembers) != 1 || reopenedMembers[0].CharID != leader.ID {
		t.Fatalf("reopened members = %+v, want the leader only", reopenedMembers)
	}
	for _, c := range s2.Characters().CharactersForDivision(testDivision) {
		switch c.Name {
		case member.Name:
			if c.GuildID != nil {
				t.Fatalf("reopened removed member FK = %v, want nil (one commit carries both pieces)", *c.GuildID)
			}
		case leader.Name:
			if c.GuildID == nil || *c.GuildID != id {
				t.Fatal("reopened leader FK lost")
			}
		}
	}
}

// TestDissolveGuildClearsEverythingAtomically proves the dissolution
// door's whole point: the guild row, the WHOLE member set and BOTH
// member characters' GuildID FKs are gone - immediately and across a
// reopen (the DELETE-only dissolvedGuilds commit leg, never the
// re-inserting dirty leg) - while an unrelated guild survives intact.
// A miss (no such guild) mutates nothing and returns false, and the
// watermark keeps allocating PAST the dissolved id (never a reissue).
func TestDissolveGuildClearsEverythingAtomically(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s := openTest(t, dir, clock)
	leader := guildTestCharacter("gdislead")
	member := guildTestCharacter("gdismate")
	bystander := guildTestCharacter("gdisother")
	for _, c := range []*enterworld.Character{leader, member, bystander} {
		if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.Guilds().CreateGuild(testDivision, enterworld.GuildRecord{Name: "DissolveW"},
		enterworld.GuildMemberRecord{CharID: leader.ID, JID: uint32(leader.ID), Name: leader.Name, PermMask: 0xffffffff}, leader)
	if err != nil {
		t.Fatal(err)
	}
	if !addTestGuildMember(s, id, leader.ID, enterworld.GuildMemberRecord{CharID: member.ID, JID: uint32(member.ID), Name: member.Name, Grade: 3}) {
		t.Fatal("fixture join refused")
	}
	otherID, err := s.Guilds().CreateGuild(testDivision, enterworld.GuildRecord{Name: "SurvivorW"},
		enterworld.GuildMemberRecord{CharID: bystander.ID, JID: uint32(bystander.ID), Name: bystander.Name}, bystander)
	if err != nil {
		t.Fatal(err)
	}

	if _, refusal := s.Guilds().DissolveGuildAs(testDivision, member.ID); !refusal.Refused() {
		t.Fatal("non-leader dissolution reported success")
	}
	if _, refusal := s.Guilds().DissolveGuildAs(testDivision, leader.ID); refusal.Refused() {
		t.Fatal("dissolution of a live guild reported failure")
	}
	if _, _, ok := s.Guilds().Guild(testDivision, id); ok {
		t.Fatal("dissolved guild still readable")
	}
	if leader.GuildID != nil || member.GuildID != nil {
		t.Fatalf("FKs after dissolve = %v/%v, want both nil", leader.GuildID, member.GuildID)
	}
	if bystander.GuildID == nil || *bystander.GuildID != otherID {
		t.Fatal("the bystander's FK moved - the door must touch only the dissolved guild's members")
	}
	s.Close()

	s2 := openTest(t, dir, clock)
	if _, _, ok := s2.Guilds().Guild(testDivision, id); ok {
		t.Fatal("dissolved guild rows survived the reopen (the DELETE leg must persist)")
	}
	survivorGuild, survivorMembers, ok := s2.Guilds().Guild(testDivision, otherID)
	if !ok || survivorGuild.Name != "SurvivorW" || len(survivorMembers) != 1 {
		t.Fatalf("survivor guild = %+v members %+v, want it intact", survivorGuild, survivorMembers)
	}
	for _, c := range s2.Characters().CharactersForDivision(testDivision) {
		switch c.Name {
		case leader.Name, member.Name:
			if c.GuildID != nil {
				t.Fatalf("reopened %s FK = %v, want nil (one commit carries every piece)", c.Name, *c.GuildID)
			}
		case bystander.Name:
			if c.GuildID == nil || *c.GuildID != otherID {
				t.Fatal("reopened bystander FK lost")
			}
		}
	}

	// The watermark never reissues the dissolved id.
	reLeader := func() *enterworld.Character {
		for _, c := range s2.Characters().CharactersForDivision(testDivision) {
			if c.Name == leader.Name {
				return c
			}
		}
		t.Fatal("leader not reloaded")
		return nil
	}()
	nextID, err := s2.Guilds().CreateGuild(testDivision, enterworld.GuildRecord{Name: "AfterDiss"},
		enterworld.GuildMemberRecord{CharID: reLeader.ID, JID: uint32(reLeader.ID), Name: reLeader.Name}, reLeader)
	if err != nil {
		t.Fatal(err)
	}
	if nextID != otherID+1 {
		t.Fatalf("post-dissolve allocation = %d, want %d (the dissolved id is never reissued)", nextID, otherID+1)
	}
}

// TestReapRefusesGuildMember proves the reaper cannot manufacture a
// leaderless or partially deleted social graph. Production reservations
// refuse all group members; this direct impossible-state fixture pins the
// reaper's fail-closed backstop.
func TestReapRefusesGuildMember(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()

	s := openTest(t, dir, clock)
	survivor := guildTestCharacter("stayguild")
	doomed := guildTestCharacter("doomguild")
	if err := s.CreateCharacter(testDivision, "test-account", survivor); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(testDivision, "test-account", doomed); err != nil {
		t.Fatal(err)
	}
	guildID, wantGuild, _ := seedTestGuild(t, s, survivor, doomed)
	s.MutateCharacter(doomed, "delete-reserve", func() {
		doomed.DeletePending = true
		doomed.DeleteReservedAt = clock.Now().UTC().Format(time.RFC3339)
	})

	clock.Advance(DeleteReservationWindow + time.Hour)
	if reaped := s.ReapMaturedDeletions(); len(reaped) != 0 {
		t.Fatalf("reaped grouped character(s) %v, want fail-closed none", reaped)
	}
	liveGuild, liveMembers, ok := s.Guilds().Guild(testDivision, guildID)
	if !ok || liveGuild != wantGuild {
		t.Fatalf("guild after reap = %+v/%v, want the intact row", liveGuild, ok)
	}
	if len(liveMembers) != 2 || liveMembers[1].CharID != doomed.ID {
		t.Fatalf("members after refused reap = %+v, want both members intact", liveMembers)
	}
	if _, ok := s.Guilds().GuildOfCharacter(testDivision, doomed.ID); !ok {
		t.Fatal("refused-reap character lost its guild")
	}
	s.MutateCharacter(doomed, "restore-impossible-reservation", func() {
		doomed.DeletePending = false
		doomed.DeleteReservedAt = ""
	})
	s.Close()

	s2 := openTest(t, dir, clock)
	reopenedGuild, reopenedMembers, ok := s2.Guilds().Guild(testDivision, guildID)
	if !ok || reopenedGuild != wantGuild {
		t.Fatalf("reopened guild = %+v/%v, want the intact row", reopenedGuild, ok)
	}
	if len(reopenedMembers) != 2 || reopenedMembers[1].CharID != doomed.ID {
		t.Fatalf("reopened members = %+v, want both members intact", reopenedMembers)
	}
	if got := len(s2.Characters().CharactersForDivision(testDivision)); got != 2 {
		t.Fatalf("reopened live characters = %d, want both retained", got)
	}
}
