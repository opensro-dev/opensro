/*
===========================================================================

buff_internal_test.go - the cumulated-buff ledger rules (#2270)

The share formula, the window arithmetic and the kill hook's kind
gates, driven straight at the ledger: CampKillGrant touches no store
door and no session directory (that is its contract with the
fatal-hit door), so these tests need no fakes beyond characters.

===========================================================================
*/

package mentor

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

const buffTestDivision = "buff-test"

/*
================
buffTestCharacter
================
*/
func buffTestCharacter(id, level int64) *enterworld.Character {
	return &enterworld.Character{ID: id, Level: &level}
}

/*
================
TestBuffKillPointsTable

The native share: min(30, trunc(10 * exp / monsterExp)), read at the
x87 posture - the denominator crosses float32, the rest is double.
================
*/
func TestBuffKillPointsTable(t *testing.T) {
	cases := []struct {
		name string
		exp  int64
		base int64
		want uint32
	}{
		{"no grant", 0, 1000, 0},
		{"no monster exp", 1000, 0, 0},
		{"full share", 1000, 1000, 10},
		{"third share truncates", 333, 1000, 3},
		{"tenth share", 100, 1000, 1},
		{"sub-point share", 50, 1000, 0},
		{"cap binds", 10000, 100, 30},
		{"cap edge exact", 300, 100, 30},
		{"cap edge under", 299, 100, 29},
		{"negative grant", -5, 1000, 0},
	}
	for _, tc := range cases {
		if got := buffKillPoints(tc.exp, tc.base); got != tc.want {
			t.Errorf("%s: buffKillPoints(%d, %d) = %d, want %d", tc.name, tc.exp, tc.base, got, tc.want)
		}
	}
}

/*
================
TestBuffRemainingAndCadence

The 1440-minute window: full at the stamp, 0 at the edge, a future
stamp (clock skew) reads as a full window. The cadence fires on
multiples of 10 and at 10 or below.
================
*/
func TestBuffRemainingAndCadence(t *testing.T) {
	const t0 = int64(1_762_934_400_000)
	remaining := []struct {
		start, now int64
		want       int64
	}{
		{0, t0, 0},
		{t0, t0, 1440},
		{t0, t0 + 60_000, 1439},
		{t0, t0 + 1439*60_000, 1},
		{t0, t0 + 1440*60_000, 0},
		{t0, t0 + 2000*60_000, 0},
		{t0 + 60_000, t0, 1440},
	}
	for _, tc := range remaining {
		if got := buffRemainingMinutes(tc.start, tc.now); got != tc.want {
			t.Errorf("buffRemainingMinutes(%d, %d) = %d, want %d", tc.start, tc.now, got, tc.want)
		}
	}
	cadence := []struct {
		remaining int64
		want      bool
	}{
		{1440, true},
		{1439, false},
		{1430, true},
		{11, false},
		{10, true},
		{1, true},
		{0, true},
	}
	for _, tc := range cadence {
		if got := buffCadenceDue(tc.remaining); got != tc.want {
			t.Errorf("buffCadenceDue(%d) = %v, want %v", tc.remaining, got, tc.want)
		}
	}
}

/*
================
TestCampKillGrantLedgerRules

One camp in the ledger: the student earns and shows state 0 on the
first grant; the sub-mentor and the level-40 student earn and drain
nothing; the master earns nothing but drains the first received row
10 points a kill for the capped share bonus, removing it at zero.
================
*/
func TestCampKillGrantLedgerRules(t *testing.T) {
	const t0 = int64(1_762_934_400_000)
	rt := &InviteRuntime{}
	rt.hydrateBuffCamp(buffTestDivision, 7, []domain.TrainingCampMemberRecord{
		{CharID: 10, Kind: MemberKindMaster},
		{CharID: 11, Kind: MemberKindSubMentor},
		{CharID: 12, Kind: MemberKindStudent},
		{CharID: 13, Kind: MemberKindStudent},
	})

	// The student's first earning kill shows state 0 with the value.
	student := buffTestCharacter(12, 20)
	bonusExp, bonusSkill, frames := rt.CampKillGrant(buffTestDivision, student, 1000, 500, 1000, t0)
	if bonusExp != 0 || bonusSkill != 0 {
		t.Fatalf("student bonus = %d/%d, want 0/0 (no received row held)", bonusExp, bonusSkill)
	}
	if len(frames) != 1 || frames[0].Opcode != OpTCBuffState ||
		string(frames[0].Payload) != string(EncodeBuffStateShow3CC1(BuffStateKillRefresh, 1440, 10)) {
		t.Fatalf("first-grant frames = %v, want the state-0 show of 10 points", frames)
	}
	// Later kills accumulate silently.
	if _, _, frames := rt.CampKillGrant(buffTestDivision, student, 1000, 500, 1000, t0); len(frames) != 0 {
		t.Fatalf("second kill frames = %v, want none", frames)
	}
	// The sub-mentor (kind 1) neither earns nor drains.
	sub := buffTestCharacter(11, 30)
	if bonus, _, frames := rt.CampKillGrant(buffTestDivision, sub, 1000, 500, 1000, t0); bonus != 0 || len(frames) != 0 {
		t.Fatalf("sub-mentor kill = bonus %d frames %v, want nothing", bonus, frames)
	}
	// A student at the graduation band earns nothing.
	senior := buffTestCharacter(13, buffLevelCap)
	if bonus, _, frames := rt.CampKillGrant(buffTestDivision, senior, 1000, 500, 1000, t0); bonus != 0 || len(frames) != 0 {
		t.Fatalf("level-40 student kill = bonus %d frames %v, want nothing", bonus, frames)
	}

	// The master drains the first received row: 25 points over three
	// kills, 10 points of drain feeding 10 % of each grant, the row
	// removed by the kill that empties it.
	masterKey := buffLedgerKey{divisionID: buffTestDivision, campID: 7, charID: 10}
	rt.buffMu.Lock()
	rt.buffLedger[masterKey].state.Reserved = []domain.TrainingCampReservedBuff{
		{DonorCharID: 12, Points: 25, WindowStartMs: t0},
	}
	rt.buffMu.Unlock()
	master := buffTestCharacter(10, 60)
	// The third kill drains only the row's last 5 points, so its bonus
	// is 5 % of the grant, not 10 %.
	for kill, wantBonus := range []int64{200, 200, 100, 0} {
		bonusExp, bonusSkill, frames := rt.CampKillGrant(buffTestDivision, master, 2000, 1000, 2000, t0)
		if bonusExp != wantBonus || bonusSkill != wantBonus/2 {
			t.Fatalf("master kill %d bonus = %d/%d, want %d/%d", kill, bonusExp, bonusSkill, wantBonus, wantBonus/2)
		}
		if kill == 2 {
			want := EncodeBuffListB00C(BuffListRemove, []BuffListRow{{Minutes: 0, MemberID: buffMemberID(12), Value: 0}})
			if len(frames) != 1 || frames[0].Opcode != OpTCBuffList || string(frames[0].Payload) != string(want) {
				t.Fatalf("draining kill frames = %v, want the mode-5 removal of donor 12", frames)
			}
		} else if len(frames) != 0 {
			t.Fatalf("master kill %d frames = %v, want none", kill, frames)
		}
	}
	// The master earned no saved points of his own through any of it.
	rt.buffMu.Lock()
	masterState := rt.buffLedger[masterKey].state
	studentState := rt.buffLedger[buffLedgerKey{divisionID: buffTestDivision, campID: 7, charID: 12}].state
	rt.buffMu.Unlock()
	if masterState.Live || masterState.Value != 0 || len(masterState.Reserved) != 0 {
		t.Fatalf("master state = %+v, want untouched saved buff and the row gone", masterState)
	}
	if !studentState.Live || studentState.Value != 20 || studentState.WindowStartMs != t0 {
		t.Fatalf("student state = %+v, want 20 points live since %d", studentState, t0)
	}
}

/*
================
TestCampKillGrantOutsideCamp

A character the ledger does not know (never admitted, or the camp
dissolved under them) earns and drains nothing, and a nil receiver
is inert - the action lane may call the hook unconditionally.
================
*/
func TestCampKillGrantOutsideCamp(t *testing.T) {
	const t0 = int64(1_762_934_400_000)
	rt := &InviteRuntime{}
	stranger := buffTestCharacter(99, 20)
	if bonus, _, frames := rt.CampKillGrant(buffTestDivision, stranger, 1000, 500, 1000, t0); bonus != 0 || frames != nil {
		t.Fatalf("stranger kill = bonus %d frames %v, want nothing", bonus, frames)
	}
	var nilRuntime *InviteRuntime
	if bonus, _, frames := nilRuntime.CampKillGrant(buffTestDivision, stranger, 1000, 500, 1000, t0); bonus != 0 || frames != nil {
		t.Fatalf("nil runtime kill = bonus %d frames %v, want nothing", bonus, frames)
	}
	// A wire-level guard for the frame the hook composes: the state-0
	// show is 9 bytes, state byte first.
	if got := EncodeBuffStateShow3CC1(BuffStateKillRefresh, 1440, 10); len(got) != 9 || got[0] != 0 {
		t.Fatalf("state-0 show = % X, want 9 bytes starting with the state", got)
	}
}
