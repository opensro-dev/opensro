/*
===========================================================================

notes.go - F1 notes.json: the player-facing patch notes of a release

One entry per merged change a player notices, in the words its pull
request gave under "## Player note" (.github/pull_request_template.md).
The Node stage fills the entries from the merges since the previous
release (--since); this file owns the row type and its schema. Public
text only: the template's rule keeps security details out of it.

===========================================================================
*/
package catalogue

// noteAreaEnum are the areas a player note names, the bug forum's own.
var noteAreaEnum = []string{"UI", "Party", "Combat", "Items", "Animation", "Audio", "World", "Other"}

/*
================
NoteEntry
================
*/
type NoteEntry struct {
	Date string `json:"date" doc:"The merge date, YYYY-MM-DD."`
	Area string `json:"area" enum:"noteArea"`
	Text string `json:"text" doc:"One plain sentence for players, as the pull request wrote it."`
	PR   int    `json:"pr"`
}

/*
================
NotesFile
================
*/
type NotesFile struct {
	Header
	Since   string      `json:"since" doc:"The previous release's commit the notes start after; empty when the build was given none."`
	Entries []NoteEntry `json:"entries"`
}
