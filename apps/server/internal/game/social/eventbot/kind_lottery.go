/*
===========================================================================

kind_lottery.go - Lottery: a drawn round a ticket enters (#3539)

Port-only, not native (on the EventBot host, #3533): a drawn round
like the raffle, except entering costs one Lottery Ticket. The
ticket is the original's own raffle ticket (ITEM_ETC_E070919_TICKET,
a native item row the v1.150 tables already carry), and every
unique's death drops one to its kill owner (host.TicketForKill), so
the lottery rewards unique hunting. One ticket is spent at entry
and never returned: losers' tickets are the price of the draw, and
a repeat whisper spends nothing (the host refuses repeats before
the ticket door runs). The winner is drawn from the entrants at
round close and paid exactly the file's reward through the grant
door; the daily win cap is the raffle's. The pool and prize stay
the operator's call, in the schedule file's reward= row.

The file row names nothing but the reward:

	21:00 lottery rounds=1 round=2m reward=ITEM_ETC_SILK_SCROLLx10 title=Sunday Lottery

===========================================================================
*/
package eventbot

import "fmt"

// TicketCodename is the entry ticket: the original's raffle ticket
// (a native item row, CanTrade 1 in its itemdata - if the owner
// later wants it bound, that is a port-only override row, not this
// kind's business).
const TicketCodename = "ITEM_ETC_E070919_TICKET"

// lotteryKind is the Lottery plug (kinds.go's Kind + TicketedKind).
type lotteryKind struct{}

/*
================
Name
================
*/
func (lotteryKind) Name() string { return "lottery" }

/*
================
Title
================
*/
func (lotteryKind) Title() string { return "Lottery" }

/*
================
OpenLine
================
*/
func (lotteryKind) OpenLine(event Event, round int) string {
	return fmt.Sprintf("%s, round %d of %d: whisper %s with a Lottery Ticket in your bag to enter - one ticket is spent, and the winner is drawn when the round closes.",
		event.DisplayTitle(), round, event.Rounds, BotName)
}

/*
================
EntryLine
================
*/
func (lotteryKind) EntryLine(event Event, round int) string {
	return fmt.Sprintf("You are in %s round %d! Your Lottery Ticket is spent - the winner is drawn when the round closes.",
		event.DisplayTitle(), round)
}

/*
================
Judge

Every ticketed entrant is in the draw.
================
*/
func (lotteryKind) Judge(string) bool { return true }

/*
================
TicketItem

The item one entry spends (kinds.go's TicketedKind).
================
*/
func (lotteryKind) TicketItem() string { return TicketCodename }

func init() {
	RegisterKind(lotteryKind{})
}
