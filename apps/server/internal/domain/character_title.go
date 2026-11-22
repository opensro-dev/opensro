/*
===========================================================================

character_title.go - a character's title state: the title it wears

The later client's title system (#3502) keeps the worn title on the
character record (schema 49). Character embeds TitleState, so the
record's flat persisted shape and its readers stay as they were while
this file owns the state. Later slices grow it here: the titles a
character owns and the wearing rules (the title window slice), beside
the state they judge.

===========================================================================
*/

package domain

/*
================
TitleState

The title state today is the worn title: the later client's RefTitle
id its name line prints through (game/title). Zero is no title, the
native default every record starts with; choosing one is the title
window slice.
================
*/
type TitleState struct {
	TitleID uint32 `json:"titleId,omitempty"`
}
