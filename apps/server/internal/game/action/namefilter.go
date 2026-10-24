/*
===========================================================================

namefilter.go - the GameWorld's copy of the native name filter

Player-chosen names (the job alias, and pet names) pass
CGame_IsValidCharacterName (v1.188 4184A0), whose list is AbuseFilter.txt
(game/namefilter). The GameWorld shares its loaded textdata filter with
character creation before admitting requests.

===========================================================================
*/

package action

/*
================
nameAllowed

Whether name passes the native name filter. A runtime built without one
(a unit test) admits every name; the GameWorld always configures it.
================
*/
func (rt *Runtime) nameAllowed(name string) bool {
	return rt.NameFilter == nil || rt.NameFilter.Allowed(name)
}
