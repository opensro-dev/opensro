/*
===========================================================================

quest_supply.go - persisted quest supply quota and confirmation reservation

Native quest user data stores its last supply day separately from journal
objectives. A pending reservation belongs to the selection-bound NPC dialog;
it cannot be reopened on the same day after cancellation or disconnect.

===========================================================================
*/
package domain

/*
================
QuestSupplyState

Day is the native uint16 world-calendar day. Map presence distinguishes an
unspent day zero from a supply already issued on that day.
================
*/
type QuestSupplyState struct {
	Day         uint16 `json:"day"`
	Pending     bool   `json:"pending,omitempty"`
	ReservedDay uint16 `json:"reservedDay,omitempty"`
}
