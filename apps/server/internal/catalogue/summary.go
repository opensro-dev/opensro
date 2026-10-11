/*
===========================================================================

summary.go - summary.json: the completion meter (IDEA-5)

Counts per status for items, skills and quests. "unverified" is its own
band, so the meter never counts an unchecked entity as progress.

===========================================================================
*/
package catalogue

/*
================
StatusCounts
================
*/
type StatusCounts struct {
	Implemented int `json:"implemented"`
	Partial     int `json:"partial"`
	Missing     int `json:"missing"`
	Unverified  int `json:"unverified"`
	Total       int `json:"total"`
}

/*
================
SummaryFile
================
*/
type SummaryFile struct {
	Header
	Items    StatusCounts `json:"items"`
	Skills   StatusCounts `json:"skills"`
	Quests   StatusCounts `json:"quests"`
	Monsters StatusCounts `json:"monsters"`
}

/*
================
SummaryInput

Each exported kind's standings; a kind not yet exported is nil.
================
*/
type SummaryInput struct {
	Items, Skills, Quests, Monsters []Standing
}

/*
================
countStatus
================
*/
func countStatus(standings []Standing) StatusCounts {
	var counts StatusCounts
	for _, standing := range standings {
		counts.Total++
		switch standing.Status {
		case StatusImplemented:
			counts.Implemented++
		case StatusPartial:
			counts.Partial++
		case StatusMissing:
			counts.Missing++
		default:
			counts.Unverified++
		}
	}
	return counts
}

/*
================
ItemStandings
================
*/
func ItemStandings(items []Item) []Standing {
	out := make([]Standing, len(items))
	for i, item := range items {
		out[i] = item.Standing
	}
	return out
}

/*
================
Summarize

The meter for the files exported so far; a kind not yet exported counts
zero of everything.
================
*/
func Summarize(header Header, in SummaryInput) SummaryFile {
	return SummaryFile{Header: header, Items: countStatus(in.Items), Skills: countStatus(in.Skills), Quests: countStatus(in.Quests),
		Monsters: countStatus(in.Monsters)}
}
