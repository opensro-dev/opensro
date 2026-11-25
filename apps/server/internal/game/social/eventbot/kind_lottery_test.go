/*
===========================================================================

kind_lottery_test.go - the Lottery kind on a frozen clock (#3539)

Port-only, not native: entry spends one Lottery Ticket
(ITEM_ETC_E070919_TICKET, the native raffle ticket), the winner is
drawn at round close, and every unique's death drops one ticket to
its kill owner through the grant door.

===========================================================================
*/
package eventbot

import (
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

const lotterySchedule = `21:00 lottery rounds=1 round=2m reward=ITEM_ETC_SILK_SCROLLx10
`

// ticketDoors is fakeDoors plus the ticket doors: a bag count per
// character name and the kill-owner grants the host asked for.
type ticketDoors struct {
	fakeDoors
	tickets map[string]int
	takes   []string
	drops   []string
}

func (f *ticketDoors) deps() Dependencies {
	deps := f.fakeDoors.deps()
	deps.TakeItem = func(_ string, character *enterworld.Character, codename string) bool {
		f.takes = append(f.takes, character.Name+":"+codename)
		if f.tickets[character.Name] < 1 {
			return false
		}
		f.tickets[character.Name]--
		return true
	}
	deps.GrantTicket = func(_, name string) bool {
		f.drops = append(f.drops, name)
		return true
	}
	return deps
}

/*
================
TestLotteryScheduleParses

A lottery row parses; the kind titles itself when the file does not.
================
*/
func TestLotteryScheduleParses(t *testing.T) {
	schedule, err := ParseSchedule(lotterySchedule, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	event := schedule.Events[0]
	if event.Kind != "lottery" || event.DisplayTitle() != "Lottery" {
		t.Fatalf("event = %+v", event)
	}
}

/*
================
TestLotteryEntrySpendsOneTicket

No ticket, no entry; one ticket enters and is spent; a repeat
whisper spends nothing more.
================
*/
func TestLotteryEntrySpendsOneTicket(t *testing.T) {
	doors := &ticketDoors{fakeDoors: fakeDoors{wins: map[string]int{}}, tickets: map[string]int{"Rich": 1}}
	schedule, err := ParseSchedule(lotterySchedule, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	host := New(true, schedule, doors.deps())
	host.Tick(day.Add(21 * time.Hour))

	poor := host.HandleWhisper(entrant("Poor", "a2"), "hello")
	if len(poor) != 1 || !strings.Contains(poor[0], "Lottery Ticket") {
		t.Fatalf("ticketless entry answered %v", poor)
	}
	if len(doors.takes) != 1 {
		t.Fatalf("takes = %v, want the one refused attempt", doors.takes)
	}

	rich := host.HandleWhisper(entrant("Rich", "a1"), "hello")
	if len(rich) != 1 || !strings.Contains(rich[0], "You are in") {
		t.Fatalf("entry answered %v", rich)
	}
	if doors.tickets["Rich"] != 0 || len(doors.takes) != 2 || doors.takes[1] != "Rich:"+TicketCodename {
		t.Fatalf("tickets = %v takes = %v", doors.tickets, doors.takes)
	}
	again := host.HandleWhisper(entrant("Rich", "a1"), "hello again")
	if len(again) != 1 || !strings.Contains(again[0], "already in") {
		t.Fatalf("repeat answered %v", again)
	}
	if len(doors.takes) != 2 {
		t.Fatalf("repeat spent a ticket: %v", doors.takes)
	}
}

/*
================
TestLotteryDrawPaysOneEntrant

At round close the winner is drawn from the ticketed entrants and
paid exactly the file's reward.
================
*/
func TestLotteryDrawPaysOneEntrant(t *testing.T) {
	doors := &ticketDoors{fakeDoors: fakeDoors{wins: map[string]int{}}, tickets: map[string]int{"Ana": 1, "Bo": 1}}
	schedule, err := ParseSchedule(lotterySchedule, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	host := New(true, schedule, doors.deps())
	host.Tick(day.Add(21 * time.Hour))
	host.HandleWhisper(entrant("Ana", "a1"), "in")
	host.HandleWhisper(entrant("Bo", "a2"), "in")
	host.Tick(day.Add(21*time.Hour + 2*time.Minute + time.Second))

	if len(doors.grants) != 1 || doors.grants[0].codename != "ITEM_ETC_SILK_SCROLL" || doors.grants[0].count != 10 {
		t.Fatalf("grants = %v", doors.grants)
	}
	joined := strings.Join(doors.announces, "\n")
	if !strings.Contains(joined, "won Lottery round 1") {
		t.Fatalf("announces = %q", joined)
	}
}

/*
================
TestLotteryNoEntrantsNoWinner

A round nobody entered closes with no winner and no grant.
================
*/
func TestLotteryNoEntrantsNoWinner(t *testing.T) {
	doors := &ticketDoors{fakeDoors: fakeDoors{wins: map[string]int{}}, tickets: map[string]int{}}
	schedule, err := ParseSchedule(lotterySchedule, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	host := New(true, schedule, doors.deps())
	host.Tick(day.Add(21 * time.Hour))
	host.Tick(day.Add(21*time.Hour + 2*time.Minute + time.Second))
	if len(doors.grants) != 0 {
		t.Fatalf("grants = %v", doors.grants)
	}
	if joined := strings.Join(doors.announces, "\n"); !strings.Contains(joined, "ends with no winner") {
		t.Fatalf("announces = %q", joined)
	}
}

/*
================
TestLotteryTicketForKill

Every unique's death drops one ticket to its kill owner while the
host runs; a disabled host drops nothing.
================
*/
func TestLotteryTicketForKill(t *testing.T) {
	doors := &ticketDoors{fakeDoors: fakeDoors{wins: map[string]int{}}, tickets: map[string]int{}}
	schedule, err := ParseSchedule(lotterySchedule, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	host := New(true, schedule, doors.deps())
	if !host.TicketForKill("d1", "Hunter") || len(doors.drops) != 1 || doors.drops[0] != "Hunter" {
		t.Fatalf("drops = %v", doors.drops)
	}
	if host.TicketForKill("d1", "") || len(doors.drops) != 1 {
		t.Fatalf("nameless kill dropped: %v", doors.drops)
	}
	off := New(false, schedule, doors.deps())
	if off.TicketForKill("d1", "Hunter") || len(doors.drops) != 1 {
		t.Fatalf("disabled host dropped: %v", doors.drops)
	}
}

/*
================
TestRaffleEntrySpendsNothing

The ticket gate belongs to the lottery kind alone: a raffle entry
never touches the ticket door.
================
*/
func TestRaffleEntrySpendsNothing(t *testing.T) {
	doors := &ticketDoors{fakeDoors: fakeDoors{wins: map[string]int{}}, tickets: map[string]int{}}
	host := New(true, parseFixture(t), doors.deps())
	host.Tick(day.Add(19*time.Hour + 30*time.Minute))
	host.HandleWhisper(entrant("Ana", "a1"), "in")
	if len(doors.takes) != 0 {
		t.Fatalf("raffle spent tickets: %v", doors.takes)
	}
}
