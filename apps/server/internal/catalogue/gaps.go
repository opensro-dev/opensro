/*
===========================================================================

gaps.go - the hand-written part of the catalogue: known gaps

A registry says whether the server handles something; it cannot say what a
handled thing still lacks. Those notes live here, keyed by the entity's
kind and id, each in player words and each with the issue that tracks it.
A note marks its entity "partial". TestEveryGapHasAnIssue fails a note
without an issue link.

===========================================================================
*/
package catalogue

import "strconv"

/*
================
Gap
================
*/
type Gap struct {
	Text  string
	Issue string
}

// gapNotes are keyed "kind:id" (item:71, skill:1, quest:12, monster:1954).
var gapNotes = map[string][]Gap{}

/*
================
applyGaps

A known gap turns an otherwise implemented entity into a partial one.
================
*/
func applyGaps(kind string, id uint32, standing Standing) Standing {
	notes := gapNotes[gapKey(kind, id)]
	if len(notes) == 0 {
		return standing
	}
	if standing.Status == StatusImplemented || standing.Status == StatusUnverified {
		standing.Status = StatusPartial
	}
	for _, note := range notes {
		standing.Gaps = append(standing.Gaps, note.Text)
		if standing.Issue == "" {
			standing.Issue = note.Issue
		}
	}
	return standing
}

/*
================
gapKey
================
*/
func gapKey(kind string, id uint32) string {
	return kind + ":" + strconv.FormatUint(uint64(id), 10)
}
