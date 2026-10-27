package party

import (
	"fmt"
	"testing"
)

const testDivision = "global-official"

func testMember(i int) Member {
	return Member{MemberID: uint32(100000 + i), Name: fmt.Sprintf("member%d", i)}
}

func TestInvitationExpirationBoundaryReplacementAndConsumption(t *testing.T) {
	r := NewRegistry()
	invite := PendingInvite{Kind: PendingInviteForm, InviterName: "member1"}
	r.SetPendingInviteAt(testDivision, "member2", invite, 1000)
	if got := r.ExpirePendingInvites(31000); len(got) != 0 {
		t.Fatal("expired at equality")
	}
	r.SetPendingInviteAt(testDivision, "member2", invite, 31000)
	if got := r.ExpirePendingInvites(31001); len(got) != 0 {
		t.Fatal("replacement inherited old deadline")
	}
	if got := r.ExpirePendingInvites(61001); len(got) != 1 || got[0].targetName != "member2" {
		t.Fatalf("expiration = %+v", got)
	}
	if got := r.ExpirePendingInvites(62000); len(got) != 0 {
		t.Fatal("duplicate expiration")
	}
	r.SetPendingInviteAt(testDivision, "member2", invite, 1000)
	r.TakePendingInvite(testDivision, "member2")
	r.SetPendingInviteAt(testDivision, "member3", invite, 1000)
	r.DropPendingInviteFor(testDivision, "member3")
	if got := r.ExpirePendingInvites(62000); len(got) != 0 {
		t.Fatal("consumed or cross-lane-dismissed invitation expired")
	}
}

// formTestParty forms a party of n members (leader = member 1) and
// returns the registry.
func formTestParty(t *testing.T, n int) *Registry {
	t.Helper()
	registry := NewRegistry()
	if _, refusal := registry.Form(testDivision, testMember(1), testMember(2), PartyOptionExpShare); refusal != "" {
		t.Fatalf("Form: %s", refusal)
	}
	for i := 3; i <= n; i++ {
		if _, refusal := registry.Join(testDivision, testMember(1).Name, testMember(i)); refusal != "" {
			t.Fatalf("Join(member%d): %s", i, refusal)
		}
	}
	return registry
}

func TestRegistryPendingInviteLifecycle(t *testing.T) {
	registry := NewRegistry()
	if registry.PendingInviteCount() != 0 {
		t.Fatalf("PendingInviteCount = %d, want 0", registry.PendingInviteCount())
	}

	// A consent with nothing outstanding takes nothing (never invited /
	// already answered / duplicate).
	if _, ok := registry.TakePendingInvite(testDivision, "member2"); ok {
		t.Fatal("TakePendingInvite resolved with nothing outstanding")
	}

	registry.SetPendingInvite(testDivision, "member2", PendingInvite{
		Kind: PendingInviteForm, InviterName: "member1", OptionBits: 0x03,
	})
	if registry.PendingInviteCount() != 1 {
		t.Fatalf("PendingInviteCount = %d, want 1", registry.PendingInviteCount())
	}

	// A second proposal REPLACES the first (the client's
	// CIFConfirmBox_RetirePeerPrompts prior-prompt dismiss: the latest
	// prompt is the only answerable one).
	registry.SetPendingInvite(testDivision, "MEMBER2", PendingInvite{
		Kind: PendingInviteJoin, InviterName: "member3",
	})
	if registry.PendingInviteCount() != 1 {
		t.Fatalf("PendingInviteCount after replace = %d, want 1", registry.PendingInviteCount())
	}

	// Take consumes case-insensitively (the bind-key shape) and yields the
	// REPLACING invite.
	invite, ok := registry.TakePendingInvite(testDivision, "Member2")
	if !ok {
		t.Fatal("TakePendingInvite missed the outstanding invitation")
	}
	if invite.Kind != PendingInviteJoin || invite.InviterName != "member3" {
		t.Fatalf("TakePendingInvite = %+v, want the replacing join invite", invite)
	}
	// The SECOND take (a duplicate/stale consent) finds nothing.
	if _, ok := registry.TakePendingInvite(testDivision, "member2"); ok {
		t.Fatal("a duplicate consent found a second pending invitation")
	}
	if registry.PendingInviteCount() != 0 {
		t.Fatalf("PendingInviteCount after take = %d, want 0", registry.PendingInviteCount())
	}
}

func TestRegistryDropPendingInviteFor(t *testing.T) {
	registry := NewRegistry()
	registry.SetPendingInvite(testDivision, "member2", PendingInvite{
		Kind: PendingInviteForm, InviterName: "member1",
	})

	// A different character's session boundary drops nothing.
	if registry.DropPendingInviteFor(testDivision, "member1") {
		t.Fatal("DropPendingInviteFor dropped an invitation the character was not the target of")
	}
	// A different division drops nothing.
	if registry.DropPendingInviteFor("other-division", "member2") {
		t.Fatal("DropPendingInviteFor dropped across divisions")
	}
	// The target's own session boundary drops the prompt.
	if !registry.DropPendingInviteFor(testDivision, "MEMBER2") {
		t.Fatal("DropPendingInviteFor missed the target's invitation")
	}
	if _, ok := registry.TakePendingInvite(testDivision, "member2"); ok {
		t.Fatal("a consent after the session boundary still found the invitation")
	}
}

func TestRegistryFormAndQueries(t *testing.T) {
	registry := NewRegistry()
	snapshot, refusal := registry.Form(testDivision, testMember(1), testMember(2), 0xFF)
	if refusal != "" {
		t.Fatalf("Form: %s", refusal)
	}
	if snapshot.LeaderID != 100001 {
		t.Fatalf("LeaderID = %d, want 100001", snapshot.LeaderID)
	}
	// The option bits mask to the three pinned flags.
	if snapshot.OptionBits != PartyOptionMask {
		t.Fatalf("OptionBits = %#02x, want %#02x", snapshot.OptionBits, PartyOptionMask)
	}
	if len(snapshot.Members) != 2 || snapshot.Members[0] != testMember(1) || snapshot.Members[1] != testMember(2) {
		t.Fatalf("Members = %+v, want leader-first pair", snapshot.Members)
	}
	if registry.Count() != 1 {
		t.Fatalf("Count = %d, want 1", registry.Count())
	}

	// Both members resolve to the same party, case-insensitively (the
	// bind-key shape).
	for _, name := range []string{"member1", "MEMBER2"} {
		got, ok := registry.PartyOf(testDivision, name)
		if !ok {
			t.Fatalf("PartyOf(%s) missed", name)
		}
		if got.LeaderID != 100001 {
			t.Fatalf("PartyOf(%s).LeaderID = %d, want 100001", name, got.LeaderID)
		}
	}
	// A different division does not resolve.
	if _, ok := registry.PartyOf("other-division", "member1"); ok {
		t.Fatal("PartyOf resolved across divisions")
	}
	if _, ok := registry.PartyOf(testDivision, "stranger"); ok {
		t.Fatal("PartyOf resolved a non-member")
	}
}

func TestRegistryFormRefusals(t *testing.T) {
	registry := formTestParty(t, 2)
	if _, refusal := registry.Form(testDivision, testMember(1), testMember(3), 0); refusal == "" {
		t.Fatal("partied leader re-formed")
	}
	if _, refusal := registry.Form(testDivision, testMember(3), testMember(2), 0); refusal == "" {
		t.Fatal("partied target joined a second party")
	}
	if _, refusal := registry.Form(testDivision, testMember(3), testMember(3), 0); refusal == "" {
		t.Fatal("self-party formed")
	}
	if registry.Count() != 1 {
		t.Fatalf("Count = %d after refusals, want 1", registry.Count())
	}
}

func TestRegistryJoin(t *testing.T) {
	registry := formTestParty(t, 2)
	snapshot, refusal := registry.Join(testDivision, "member2", testMember(3))
	if refusal != "" {
		t.Fatalf("Join: %s", refusal)
	}
	if len(snapshot.Members) != 3 || snapshot.Members[2] != testMember(3) {
		t.Fatalf("Members = %+v, want member3 appended", snapshot.Members)
	}

	// Refusals: joiner already partied, actor partyless.
	if _, refusal := registry.Join(testDivision, "member1", testMember(2)); refusal == "" {
		t.Fatal("partied joiner re-joined")
	}
	if _, refusal := registry.Join(testDivision, "stranger", testMember(4)); refusal == "" {
		t.Fatal("partyless actor extended a party")
	}
}

func TestRegistryJoinCap(t *testing.T) {
	registry := formTestParty(t, PartyMaxMembers)
	if _, refusal := registry.Join(testDivision, "member1", testMember(PartyMaxMembers+1)); refusal == "" {
		t.Fatalf("ninth member joined past the %d cap", PartyMaxMembers)
	}
	snapshot, _ := registry.PartyOf(testDivision, "member1")
	if len(snapshot.Members) != PartyMaxMembers {
		t.Fatalf("Members = %d, want %d", len(snapshot.Members), PartyMaxMembers)
	}
}

func TestRegistryJoinCapacityByExperienceOption(t *testing.T) {
	for options := uint8(0); options < 8; options++ {
		t.Run(fmt.Sprintf("options-%d", options), func(t *testing.T) {
			registry := NewRegistry()
			if _, refusal := registry.Form(testDivision, testMember(1), testMember(2), options); refusal != "" {
				t.Fatal(refusal)
			}
			want := 4
			if options&1 != 0 {
				want = 8
			}
			for i := 3; i <= want; i++ {
				if _, refusal := registry.Join(testDivision, "member1", testMember(i)); refusal != "" {
					t.Fatalf("member %d below capacity: %s", i, refusal)
				}
			}
			if _, refusal := registry.Join(testDivision, "member1", testMember(want+1)); refusal != "party is full" {
				t.Fatalf("overflow refusal = %q", refusal)
			}
			if _, exists := registry.PartyOf(testDivision, testMember(want+1).Name); exists {
				t.Fatal("rejected member acquired party ownership")
			}
			snapshot, _ := registry.PartyOf(testDivision, "member1")
			if len(snapshot.Members) != want {
				t.Fatalf("roster mutated beyond capacity: %d", len(snapshot.Members))
			}
		})
	}
}

func TestRegistryLeaveAsMember(t *testing.T) {
	registry := formTestParty(t, 3)
	outcome, refusal := registry.Leave(testDivision, "member2")
	if refusal != "" {
		t.Fatalf("Leave: %s", refusal)
	}
	if outcome.WasLeader || outcome.Dissolved {
		t.Fatalf("outcome = %+v, want plain member departure", outcome)
	}
	if outcome.Leaver != testMember(2) {
		t.Fatalf("Leaver = %+v, want member2", outcome.Leaver)
	}
	if len(outcome.Others) != 2 || outcome.Others[0] != testMember(1) || outcome.Others[1] != testMember(3) {
		t.Fatalf("Others = %+v, want members 1 and 3", outcome.Others)
	}
	if _, ok := registry.PartyOf(testDivision, "member2"); ok {
		t.Fatal("leaver still resolves to the party")
	}
	if snapshot, ok := registry.PartyOf(testDivision, "member1"); !ok || len(snapshot.Members) != 2 {
		t.Fatalf("remaining party = %+v (ok=%v), want 2 members", snapshot, ok)
	}
}

func TestRegistryLeaveAsLeaderDissolves(t *testing.T) {
	registry := formTestParty(t, 3)
	outcome, refusal := registry.Leave(testDivision, "member1")
	if refusal != "" {
		t.Fatalf("Leave: %s", refusal)
	}
	if !outcome.WasLeader || !outcome.Dissolved {
		t.Fatalf("outcome = %+v, want leader dissolve", outcome)
	}
	if len(outcome.Others) != 2 {
		t.Fatalf("Others = %+v, want the two remaining members", outcome.Others)
	}
	if registry.Count() != 0 {
		t.Fatalf("Count = %d after dissolve, want 0", registry.Count())
	}
}

func TestRegistryLeaveBelowMinimumDissolves(t *testing.T) {
	registry := formTestParty(t, 2)
	outcome, refusal := registry.Leave(testDivision, "member2")
	if refusal != "" {
		t.Fatalf("Leave: %s", refusal)
	}
	if outcome.WasLeader {
		t.Fatalf("outcome = %+v, want non-leader departure", outcome)
	}
	if !outcome.Dissolved {
		t.Fatal("two-member party survived a departure")
	}
	if registry.Count() != 0 {
		t.Fatalf("Count = %d after dissolve, want 0", registry.Count())
	}
}

func TestRegistryLeaveRefusals(t *testing.T) {
	registry := NewRegistry()
	if _, refusal := registry.Leave(testDivision, "member1"); refusal == "" {
		t.Fatal("partyless leave applied")
	}
}

func TestRegistryBanish(t *testing.T) {
	registry := formTestParty(t, 3)
	outcome, refusal := registry.Banish(testDivision, "member1", 100003)
	if refusal != "" {
		t.Fatalf("Banish: %s", refusal)
	}
	if outcome.Leaver != testMember(3) {
		t.Fatalf("Leaver = %+v, want member3", outcome.Leaver)
	}
	if outcome.Dissolved {
		t.Fatal("three-member party dissolved on one banish")
	}
	if len(outcome.Others) != 2 {
		t.Fatalf("Others = %+v, want members 1 and 2", outcome.Others)
	}
	if _, ok := registry.PartyOf(testDivision, "member3"); ok {
		t.Fatal("banished member still resolves to the party")
	}
}

func TestRegistryBanishBelowMinimumDissolves(t *testing.T) {
	registry := formTestParty(t, 2)
	outcome, refusal := registry.Banish(testDivision, "member1", 100002)
	if refusal != "" {
		t.Fatalf("Banish: %s", refusal)
	}
	if !outcome.Dissolved {
		t.Fatal("two-member party survived a banish")
	}
	if registry.Count() != 0 {
		t.Fatalf("Count = %d after dissolve, want 0", registry.Count())
	}
}

func TestRegistryBanishRefusals(t *testing.T) {
	registry := formTestParty(t, 3)
	if _, refusal := registry.Banish(testDivision, "member2", 100003); refusal == "" {
		t.Fatal("non-leader banished")
	}
	if _, refusal := registry.Banish(testDivision, "member1", 100001); refusal == "" {
		t.Fatal("leader self-banished")
	}
	if _, refusal := registry.Banish(testDivision, "member1", 999999); refusal == "" {
		t.Fatal("unknown member id banished")
	}
	if _, refusal := registry.Banish(testDivision, "stranger", 100002); refusal == "" {
		t.Fatal("partyless actor banished")
	}
	if snapshot, _ := registry.PartyOf(testDivision, "member1"); len(snapshot.Members) != 3 {
		t.Fatalf("Members = %+v after refusals, want 3", snapshot.Members)
	}
}

func TestRegistrySnapshotIsolation(t *testing.T) {
	registry := formTestParty(t, 2)
	snapshot, _ := registry.PartyOf(testDivision, "member1")
	snapshot.Members[0] = testMember(9)
	fresh, _ := registry.PartyOf(testDivision, "member1")
	if fresh.Members[0] != testMember(1) {
		t.Fatal("snapshot mutation leaked into the registry")
	}
}
