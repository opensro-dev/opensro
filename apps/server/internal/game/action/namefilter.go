/*
===========================================================================

namefilter.go - the GameWorld's copy of the native name filter

Player-chosen names (the job alias, and pet names) pass
CGame_IsValidCharacterName (v1.188 4184A0), whose list is AbuseFilter.txt
(game/namefilter). The GameWorld loads the projection's copy at boot.

===========================================================================
*/

package action

import (
	"fmt"
	"os"
	"path/filepath"

	"opensro.online/server/internal/game/namefilter"
)

/*
================
ConfigureNameFilter

A missing or malformed filter fails construction: names would otherwise
pass unchecked.
================
*/
func (rt *Runtime) ConfigureNameFilter(textdataDir string) error {
	data, err := os.ReadFile(filepath.Join(textdataDir, namefilter.File))
	if err != nil {
		return fmt.Errorf("name filter: %w", err)
	}
	filter, err := namefilter.Parse(data)
	if err != nil {
		return fmt.Errorf("name filter: %w", err)
	}
	rt.NameFilter = filter
	return nil
}

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
