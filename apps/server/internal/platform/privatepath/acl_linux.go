/*
===========================================================================

acl_linux.go - POSIX access ACL detection on Linux

Linux stores a path's extended access ACL in the system.posix_acl_access
extended attribute; a path with only the three mode classes has none.
A file system without ACL support reports ENOTSUP, which means no ACL.

===========================================================================
*/

package privatepath

import (
	"errors"

	"golang.org/x/sys/unix"
)

const accessACLAttr = "system.posix_acl_access"

/*
================
hasAccessACL

Reports whether path carries an extended access ACL.
================
*/
func hasAccessACL(path string) (bool, error) {
	_, err := unix.Getxattr(path, accessACLAttr, nil)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
		return false, nil
	}
	return false, err
}
