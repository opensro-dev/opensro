/*
===========================================================================

amount.go - codename-based requests to the inventory grant owner

Binding is explicit grant metadata, never inferred from an item's reference.

===========================================================================
*/
package inventory

// ItemAmount is a version-independent authored grant or consumption request.
// The item authority resolves its codename against this world's item catalog.
/*
================
ItemAmount
================
*/
type ItemAmount struct {
	// Port-only, not native: only explicitly bound grants set this field.
	CharacterBound bool
	Codename       string
	Count          uint32
}
