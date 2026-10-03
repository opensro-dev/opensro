/*
===========================================================================

expiry_test.go - the duration clock and the expiry update

===========================================================================
*/

package statuseffect

import "testing"

func TestEffectExpiryPoliciesAndZeroPresence(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    Effect
		at   int64
		want bool
	}{
		{"ordinary-before", Effect{ExpiresAtMs: 100}, 99, false},
		{"ordinary-equal", Effect{ExpiresAtMs: 100}, 100, false},
		{"ordinary-after", Effect{ExpiresAtMs: 100}, 101, true},
		{"imbue-equal", Effect{ExpiresAtMs: 100, Imbue: true}, 100, false},
		{"job-equal", Effect{ExpiresAtMs: 100, Persistent: true}, 100, true},
		{"absent", Effect{}, 100, false},
		{"zero-equal", Effect{DurationPresent: true}, 0, false},
		{"zero-after", Effect{DurationPresent: true}, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e.Expired(tc.at); got != tc.want {
				t.Fatalf("expired=%v want %v", got, tc.want)
			}
		})
	}
}

func TestNativeDurationClockWrapAndProjection(t *testing.T) {
	start := int64(0xfffffff0)
	e := Effect{DurationPresent: true, StartedAtMs: start, ExpiresAtMs: start + 32}
	for _, c := range []struct {
		at        int64
		expired   bool
		remaining uint32
	}{
		{start, false, 32}, {start + 31, false, 1}, {start + 32, false, 0}, {start + 33, true, 0},
		// The native counter and elapsed subtraction are uint32. Retain that
		// behavior even for a detached snapshot after an entire counter cycle.
		{start + 0x100000000, false, 32}, {start + 0x100000020, false, 0},
		// A reader whose clock trails the install sees a row not yet started:
		// live, its whole duration left (never a wrapped elapsed).
		{start - 1, false, 32},
	} {
		if e.Expired(c.at) != c.expired || e.RemainingMs(c.at) != c.remaining {
			t.Fatalf("at %x expired %v remaining %d", c.at, e.Expired(c.at), e.RemainingMs(c.at))
		}
	}
}

const (
	lateInstallStartMs  = 1000 // the operation's clock at installation
	lateInstallTickMs   = 970  // the tick's clock, sampled before the install
	lateInstallDuration = 1800000
	// lateInstallSkill is SKILL_EU_WARRIOR_GUARDA_INTERCEPT_A_01 in its
	// own group, apart from the link fixture's skill and group.
	lateInstallSkill = 7260
	lateInstallGroup = 419
	// lateInstallToken is distinct from the link fixture's two tokens.
	lateInstallToken = 20
	// lateInstallRows is the timed effect plus both link halves.
	lateInstallRows = 3
)

/*
==================
TestExpireLeavesAnInstallNewerThanItsClock

A tick samples its clock, then an operation installs a timed effect and a
linked pair on a later clock before the tick's update runs. That update
must not read the negative elapsed as a wrapped uint32 and retire them;
the next update past their duration still does.
==================
*/
func TestExpireLeavesAnInstallNewerThanItsClock(t *testing.T) {
	r := NewRegistry()
	timed := Effect{DivisionID: "g", CharacterName: "friend", SkillID: lateInstallSkill, SkillGroup: lateInstallGroup, InstanceToken: lateInstallToken,
		State: StateActive, DurationPresent: true, StartedAtMs: lateInstallStartMs, ExpiresAtMs: lateInstallStartMs + lateInstallDuration}
	if !r.Apply(timed) {
		t.Fatal("apply")
	}
	l := testLink()
	l.StartedAtMs, l.ExpiresAtMs = lateInstallStartMs, lateInstallStartMs+lateInstallDuration
	if code := r.ApplyLink(l); code != 0 {
		t.Fatal(code)
	}

	r.Expire(lateInstallTickMs)
	if ended := r.DrainStopRequested(); len(ended) != 0 {
		t.Fatalf("the update retired installs newer than its clock: %d owners", len(ended))
	}
	if _, ok := r.ThreatLink("g", "friend", lateInstallStartMs); !ok {
		t.Fatal("the link did not survive the earlier tick")
	}

	r.Expire(lateInstallStartMs + lateInstallDuration + 1)
	ended := 0
	for _, batch := range r.DrainStopRequested() {
		ended += len(batch.Effects)
	}
	if ended != lateInstallRows {
		t.Fatalf("expired %d rows, want the timed effect and both link halves", ended)
	}
}
