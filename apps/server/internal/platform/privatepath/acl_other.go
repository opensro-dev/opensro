//go:build !windows && !linux

/*
===========================================================================

acl_other.go - access ACL detection on non-Linux Unix hosts

The shards run on Linux. Other Unix hosts (developer macOS, BSD) keep ACLs
apart from the mode bits, so chmod does not rewrite them: report none.

===========================================================================
*/

package privatepath

/*
================
hasAccessACL
================
*/
func hasAccessACL(path string) (bool, error) {
	return false, nil
}
