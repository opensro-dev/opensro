/*
===========================================================================

switch.go - the one parser for a commerce on/off setting

Every commerce switch reads its environment variable the same way: the
on and off spellings, an unset value that takes the switch's own default,
and a boot error naming the setting for anything else, so a typo can
never silently open or close a shop.

===========================================================================
*/
package commerce

import (
	"fmt"
	"os"
	"strings"
)

/*
================
switchFromEnv

The setting name's value as a switch. unset is what an empty or missing
value means: false for a switch whose native state is off, true for an
owner default-on one.
================
*/
func switchFromEnv(name string, unset bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "":
		return unset, nil
	case "1", "on", "true":
		return true, nil
	case "0", "off", "false":
		return false, nil
	}
	return false, fmt.Errorf("%s=%q: want on or off", name, os.Getenv(name))
}
