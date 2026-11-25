/*
===========================================================================

churn.go - the accounts that open the most sessions (#3307)

The 2026-11-12 investigation found one account opening three sessions a
second for a day, and four accounts writing 97% of the journal, and
nothing said so. This read ranks the accounts by the sessions they
entered in a window, with the two peaks a rate alert reads: the busiest
UTC minute of the account's entries, and the busiest UTC hour of any one
of its characters.

Facts only, as every census answer: counts and peaks, no threshold and
no verdict. The monitor (opensro-ops) and the console decide what a
number means. Like the rest of the census it reads the journal snapshot
the other routes share (Service.snapshot), so it costs one pass over that
window's spans and no store access.

===========================================================================
*/
package census

import (
	"sort"
	"strings"
)

const (
	churnMinuteMs = 60 * 1000
	churnHourMs   = 60 * churnMinuteMs
	// churnMax bounds the ranked list; truncated says when more entered.
	churnMax = 100
)

// churnWindows maps the churn route's window names to milliseconds.
var churnWindows = map[string]int64{
	"1h":  churnHourMs,
	"24h": windowDayMs,
}

/*
================
AccountChurn

One account's entries in the window. Sessions counts the spans that
began in it; PeakMinute is the most of them begun in one UTC minute,
PeakCharacterHour the most begun by one character in one UTC hour, and
NoUserAgent how many came from no browser (signals.go).
================
*/
type AccountChurn struct {
	Account           string `json:"account"`
	Sessions          int    `json:"sessions"`
	Characters        int    `json:"characters"`
	PeakMinute        int    `json:"peakMinute"`
	PeakCharacterHour int    `json:"peakCharacterHour"`
	NoUserAgent       int    `json:"noUserAgent"`
}

/*
================
Churn

The churn answer: the window, the shard's entries and accounts in it,
and the accounts by sessions, most first.
================
*/
type Churn struct {
	SchemaVersion int            `json:"schemaVersion"`
	GeneratedAt   int64          `json:"generatedAt"`
	WindowMs      int64          `json:"windowMs"`
	Sessions      int            `json:"sessions"`
	Accounts      int            `json:"accounts"`
	Top           []AccountChurn `json:"top"`
	Truncated     bool           `json:"truncated"`
}

/*
================
churnTally

One account's running counts during the pass: entries per minute and
per character-hour are keyed by bucket start, so a peak is one map
read at the end.
================
*/
type churnTally struct {
	row        AccountChurn
	characters map[string]bool
	minutes    map[int64]int
	charHours  map[characterHour]int
}

// characterHour is one character's UTC hour: the entries-per-character
// rate's bucket.
type characterHour struct {
	character string
	hourMs    int64
}

/*
================
Service.Churn

The accounts by sessions entered in the windowMs behind now.
================
*/
func (s *Service) Churn(windowMs int64) (Churn, error) {
	idx, err := s.snapshot()
	if err != nil {
		return Churn{}, err
	}
	now := s.nowMs()
	answer := Churn{SchemaVersion: SchemaVersion, GeneratedAt: now, WindowMs: windowMs, Top: []AccountChurn{}}
	tallies := map[string]*churnTally{}
	for _, span := range idx.spans {
		if span.Started < now-windowMs || span.Started >= now {
			continue
		}
		tally := tallies[span.Account]
		if tally == nil {
			tally = &churnTally{row: AccountChurn{Account: span.Account}, characters: map[string]bool{},
				minutes: map[int64]int{}, charHours: map[characterHour]int{}}
			tallies[span.Account] = tally
		}
		character := strings.ToLower(span.Character)
		tally.row.Sessions++
		tally.characters[character] = true
		tally.minutes[span.Started-span.Started%churnMinuteMs]++
		tally.charHours[characterHour{character, span.Started - span.Started%churnHourMs}]++
		if span.NoUserAgent {
			tally.row.NoUserAgent++
		}
		answer.Sessions++
	}
	rows := make([]AccountChurn, 0, len(tallies))
	for _, tally := range tallies {
		tally.row.Characters = len(tally.characters)
		for _, n := range tally.minutes {
			tally.row.PeakMinute = max(tally.row.PeakMinute, n)
		}
		for _, n := range tally.charHours {
			tally.row.PeakCharacterHour = max(tally.row.PeakCharacterHour, n)
		}
		rows = append(rows, tally.row)
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].Sessions != rows[b].Sessions {
			return rows[a].Sessions > rows[b].Sessions
		}
		if rows[a].PeakMinute != rows[b].PeakMinute {
			return rows[a].PeakMinute > rows[b].PeakMinute
		}
		return rows[a].Account < rows[b].Account
	})
	answer.Accounts = len(rows)
	answer.Truncated = len(rows) > churnMax
	if answer.Truncated {
		rows = rows[:churnMax]
	}
	answer.Top = append(answer.Top, rows...)
	return answer, nil
}
