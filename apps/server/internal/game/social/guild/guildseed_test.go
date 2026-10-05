package guild_test

// The Phase C/D seed pins: an IN-GUILD character's enter-world stream
// carries exactly one 0x32C4 whose bytes match a hand-rolled oracle
// (built with encoding/binary here, NEVER the production encoder - the
// encoder must match the pinned sub_826610 layout, not itself), with the
// offline flags derived from live presence (0 = online, 1 = offline);
// and a DANGLING GuildID (an FK pointing at no stored guild row) emits
// NO 0x32C4 at all - absence over invention. The no-guild pin itself
// lives in noguildentry_test.go and must survive untouched.

import (
	"bytes"
	"encoding/binary"
	"opensro.online/server/internal/domain"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	presence "opensro.online/server/internal/game/social"
	"opensro.online/server/internal/game/social/community"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

// staticGuildStore is a one-guild enterworld.GuildStore fake.
type staticGuildStore struct {
	division string
	guildID  int64
	guild    enterworld.GuildRecord
	members  []enterworld.GuildMemberRecord
}

func (s staticGuildStore) Guild(divisionID string, guildID int64) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
	if divisionID != s.division || guildID != s.guildID {
		return enterworld.GuildRecord{}, nil, false
	}
	members := make([]enterworld.GuildMemberRecord, len(s.members))
	copy(members, s.members)
	return s.guild, members, true
}

func (s staticGuildStore) GuildOfCharacter(divisionID string, characterID int64) (int64, bool) {
	if divisionID != s.division {
		return 0, false
	}
	for _, member := range s.members {
		if member.CharID == characterID {
			return s.guildID, true
		}
	}
	return 0, false
}

func (s staticGuildStore) CreateGuild(string, enterworld.GuildRecord, enterworld.GuildMemberRecord, *enterworld.Character) (int64, error) {
	return 0, nil
}

func (s staticGuildStore) UpdateGuildAs(string, int64, string, enterworld.GuildAuthorization, func(enterworld.GuildRecord, []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool)) (enterworld.GuildSnapshot, enterworld.GuildRefusal) {
	return enterworld.GuildSnapshot{}, enterworld.GuildRefusalUpdateRejected
}

func (s staticGuildStore) AddGuildMemberAs(string, int64, int64, uint32, enterworld.GuildMemberRecord) (enterworld.GuildSnapshot, enterworld.GuildRefusal) {
	return enterworld.GuildSnapshot{}, enterworld.GuildRefusalUpdateRejected
}

func (s staticGuildStore) KickGuildMember(string, int64, string, uint32) (enterworld.GuildRemovalResult, enterworld.GuildRefusal) {
	return enterworld.GuildRemovalResult{}, enterworld.GuildRefusalUpdateRejected
}

func (s staticGuildStore) LeaveGuild(string, int64) (enterworld.GuildRemovalResult, enterworld.GuildRefusal) {
	return enterworld.GuildRemovalResult{}, enterworld.GuildRefusalUpdateRejected
}

// DissolveGuildAs completes the enterworld.GuildStore contract the break
// door added. DECISION: inert false, matching this fake's other write
// doors (the seed tests read, never mutate).
func (s staticGuildStore) DissolveGuildAs(string, int64) (enterworld.GuildSnapshot, enterworld.GuildRefusal) {
	return enterworld.GuildSnapshot{}, enterworld.GuildRefusalUpdateRejected
}

// DonateGuildPoints completes the contract the GP-donate door added -
// inert like the other write doors.
func (s staticGuildStore) DonateGuildPoints(string, int64, uint32) (enterworld.GuildDonationResult, enterworld.GuildRefusal) {
	return enterworld.GuildDonationResult{}, enterworld.GuildRefusalUpdateRejected
}

func (s staticGuildStore) ClaimWarCompensationAs(string, int64) (int64, enterworld.GuildRefusal) {
	return 0, enterworld.GuildRefusalUpdateRejected
}

func (s staticGuildStore) OpenMasterReleaseVoteAs(string, int64, int64, func(int64) int64) (enterworld.GuildSnapshot, enterworld.GuildRefusal) {
	return enterworld.GuildSnapshot{}, enterworld.GuildRefusalUpdateRejected
}

func (s staticGuildStore) CastGuildBallotAs(string, int64, uint32, uint8) (domain.GuildVoteBallot, enterworld.GuildRefusal) {
	return domain.GuildVoteBallot{}, enterworld.GuildRefusalUpdateRejected
}

func (s staticGuildStore) CloseDueGuildVotes(string, int64, uint8, uint32) []domain.GuildVoteOutcome {
	return nil
}

func (s staticGuildStore) TransactGuildStorageAs(string, int64, func(*domain.Character, *domain.AccountStorage) error) (domain.AccountStorage, enterworld.GuildRefusal, error) {
	return domain.AccountStorage{}, enterworld.GuildRefusalUpdateRejected, nil
}

func (s staticGuildStore) LevelUpGuildAs(string, int64) (enterworld.GuildSnapshot, enterworld.GuildRefusal) {
	return enterworld.GuildSnapshot{}, enterworld.GuildRefusalUpdateRejected
}

// oracle32C4 hand-rolls the pinned 0x32C4 layout (fold sub_826610;
// little-endian, strings are {u16 len}{ANSI bytes}) with encoding/binary.
type oracle32C4 struct{ buf bytes.Buffer }

func (o *oracle32C4) u8(v uint8)   { o.buf.WriteByte(v) }
func (o *oracle32C4) u32(v uint32) { binary.Write(&o.buf, binary.LittleEndian, v) }
func (o *oracle32C4) str(v string) {
	binary.Write(&o.buf, binary.LittleEndian, uint16(len(v)))
	o.buf.WriteString(v)
}

// presenceWithOnline builds a live presence facade whose hub holds a
// bound session for the given member name (a hub needs no started
// listeners for the binding registry - NewServer only prepares them).
func presenceWithOnline(t *testing.T, divisionID, name string) *presence.Directory {
	t.Helper()
	srv, err := transport.NewServer(transport.Config{
		WTAddr:  "127.0.0.1:0",
		WSAddr:  "127.0.0.1:0",
		CertDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("transport.NewServer: %v", err)
	}
	if _, replaced := srv.Hub.BindExclusive(presence.BindKey(divisionID, name), &transport.Session{}); replaced {
		t.Fatal("fresh hub replaced a session")
	}
	return presence.NewDirectory(srv.Hub)
}

// seedTestDeps composes the enter-world deps for one in-guild character
// through the PRODUCTION seed seam (community.SeedFramesFunc over the
// guild door), the noguildentry_test.go shape.
func seedTestDeps(character *enterworld.Character, directory *presence.Directory, guilds enterworld.GuildStore) *enterworld.Deps {
	source := enterworld.StaticCharacterSource{enterworld.DefaultDivisionID: {character}}
	deps := &enterworld.Deps{
		Roster:     &enterworld.Roster{},
		Characters: source,
	}
	deps.ResolveDivisionID = enterworld.DevResolveDivisionIDFromCatalog(source)
	communitySeeds := community.SeedFramesFunc(directory, nil)
	deps.CommunitySeedFramesFor = func(divisionID string, character *enterworld.Character) []enterworld.Packet {
		return guild.AppendSeedFrame(
			communitySeeds(divisionID, character),
			guilds,
			directory,
			divisionID,
			character,
		)
	}
	return deps
}

func guildSeedCharacter(guildID int64) *enterworld.Character {
	race := int64(enterworld.RaceChina)
	gender := int64(enterworld.GenderMale)
	return &enterworld.Character{
		ID:            7,
		Name:          "guilded",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     &race,
		Gender:        &gender,
		GuildID:       &guildID,
	}
}

func framePayload(frame enterworld.Packet) []byte {
	out := make([]byte, len(frame.Payload))
	for i, v := range frame.Payload {
		out[i] = byte(v)
	}
	return out
}

func TestEnterWorldSeedsGuildInfo32C4(t *testing.T) {
	t.Parallel()
	const guildID = int64(42)
	character := guildSeedCharacter(guildID)

	// Two members: the entering character itself (NOT yet bound at seed
	// composition time - OnWorldBound claims the presence key after
	// HandleEnterWorld composes the frames - so honestly OFFLINE, flag 1)
	// and a peer with a live bound session (ONLINE, flag 0).
	store := staticGuildStore{
		division: enterworld.DefaultDivisionID,
		guildID:  guildID,
		guild: enterworld.GuildRecord{
			ID:             guildID,
			Name:           "NightWatch",
			Level:          3,
			GP:             1234,
			NoticeSubject:  "watch the wall",
			NoticeContents: "and hold it",
			CrestParam:     0x00c84433,
			Byte10:         2,
		},
		members: []enterworld.GuildMemberRecord{
			{CharID: 7, JID: 100007, Name: "guilded", Grade: 1, Level: 20, DonatedGP: 800, PermMask: 0xffffffff, Dword30: 1, Dword34: 2, Dword38: 3, GrantName: "Lord", RefObjID: 1907, FortressRole: 0},
			{CharID: 8, JID: 100008, Name: "Watcher_2", Grade: 3, Level: 9, DonatedGP: 150, PermMask: 0, Dword30: 0, Dword34: 0, Dword38: 0, GrantName: "", RefObjID: 1911, FortressRole: 0},
		},
	}
	presence := presenceWithOnline(t, enterworld.DefaultDivisionID, "Watcher_2")
	deps := seedTestDeps(character, presence, store)

	outcome := enterworld.HandleEnterWorld(deps, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, enterworld.DefaultDivisionID, "guilded"),
	))
	if !outcome.OK {
		t.Fatalf("enter world failed: %+v", outcome.Result)
	}

	// The hand-rolled oracle for the pinned layout.
	oracle := &oracle32C4{}
	oracle.u32(42)           // guildId
	oracle.str("NightWatch") // name
	oracle.u8(3)             // level
	oracle.u32(1234)         // GP
	oracle.str("watch the wall")
	oracle.str("and hold it")
	oracle.u32(0x00c84433) // crestParam
	oracle.u8(2)           // byte10
	oracle.u8(2)           // memberCount
	// member[0]: the entering character, offline at compose time.
	oracle.u32(100007)
	oracle.str("guilded")
	oracle.u8(1)  // grade
	oracle.u8(20) // level
	oracle.u32(800)
	oracle.u32(0xffffffff)
	oracle.u32(1)
	oracle.u32(2)
	oracle.u32(3)
	oracle.str("Lord")
	oracle.u32(1907)
	oracle.u8(0) // fortressRole
	oracle.u8(1) // offlineFlag: OFFLINE
	// member[1]: the bound peer, online.
	oracle.u32(100008)
	oracle.str("Watcher_2")
	oracle.u8(3) // grade
	oracle.u8(9) // level
	oracle.u32(150)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.u32(0)
	oracle.str("")
	oracle.u32(1911)
	oracle.u8(0) // fortressRole
	oracle.u8(0) // offlineFlag: ONLINE
	oracle.u8(0) // voteCount

	guildFrames := 0
	letterIndex, guildIndex := -1, -1
	for index, frame := range outcome.Frames {
		switch frame.NativeOpcode {
		case guild.OpGuildInfo:
			guildFrames++
			guildIndex = index
			if got := framePayload(frame); !bytes.Equal(got, oracle.buf.Bytes()) {
				t.Errorf("0x32C4 payload = % X, want the oracle % X", got, oracle.buf.Bytes())
			}
		case community.OpLetterListAnswer:
			letterIndex = index
		}
	}
	if guildFrames != 1 {
		t.Fatalf("0x32C4 frames in the stream = %d, want exactly 1", guildFrames)
	}
	if letterIndex < 0 || guildIndex != letterIndex+1 {
		t.Fatalf("guild frame at %d, letter frame at %d - the 0x32C4 seed must ride immediately AFTER the letter seed", guildIndex, letterIndex)
	}
}

func TestEnterWorldDanglingGuildLinkEmitsNoGuildInfo(t *testing.T) {
	t.Parallel()
	// The FK points at guild 99; the store holds only guild 42: the
	// dangling link must emit NOTHING (logged loud by the seed builder) -
	// inventing an empty 0x32C4 would paint a guild onto the character.
	character := guildSeedCharacter(99)
	store := staticGuildStore{
		division: enterworld.DefaultDivisionID,
		guildID:  42,
		guild:    enterworld.GuildRecord{ID: 42, Name: "NightWatch"},
	}
	deps := seedTestDeps(character, nil, store)

	outcome := enterworld.HandleEnterWorld(deps, transport.EncodeEnterWorld(
		entryauth.NewAuthenticatedEntryFixture(t, enterworld.DefaultDivisionID, "guilded"),
	))
	if !outcome.OK {
		t.Fatalf("enter world failed: %+v", outcome.Result)
	}
	seeds := 0
	for index, frame := range outcome.Frames {
		if frame.NativeOpcode == guild.OpGuildInfo {
			t.Errorf("frame[%d] is a 0x32C4 for a DANGLING guild link - absence over invention", index)
		}
		if frame.NativeOpcode == community.OpFriendRosterPush || frame.NativeOpcode == community.OpLetterListAnswer {
			seeds++
		}
	}
	if seeds != 2 {
		t.Fatalf("community seed frames = %d, want the unchanged no-guild pair", seeds)
	}
}
