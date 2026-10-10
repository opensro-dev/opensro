/*
===========================================================================

search.go - C9 search.json: one name index over every catalogue file

Names, icons and some rows are client data, so the Node stage fills the
index once every other file is final; this file owns its row type and
schema. A skill appears once per skill group, under its first rank.

===========================================================================
*/
package catalogue

var searchKindEnum = []string{"item", "skill", "quest", "monster", "npc", "region"}

/*
================
SearchEntry
================
*/
type SearchEntry struct {
	Kind string `json:"kind" enum:"searchKind"`
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
}

/*
================
SearchFile
================
*/
type SearchFile struct {
	Header
	Entries []SearchEntry `json:"entries"`
}
