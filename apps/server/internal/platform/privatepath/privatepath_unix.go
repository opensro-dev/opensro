//go:build !windows

/*
===========================================================================

privatepath_unix.go - service-private modes on Unix hosts

A directory becomes 0700 and a file 0600. When the path carries a POSIX
access ACL, the group bits of the mode are the ACL mask, not the owning
group: chmod(2) rewrites the mask from them, and a plain 0700 would set
mask::--- and silently disable every named entry an operator granted (the
ops stall-dump watch reads the authority through one, #2860). So an ACL'd
path keeps its current mask; owner and other bits are still forced.

===========================================================================
*/

package privatepath

import "os"

const (
	privateDirMode  os.FileMode = 0o700
	privateFileMode os.FileMode = 0o600
	// aclMaskBits are the mode bits that mirror the ACL mask entry.
	aclMaskBits os.FileMode = 0o070
)

/*
================
protect

Tightens path to the private mode. A path with an access ACL keeps the
mask bits it has, so operator-granted named entries stay effective; the
named entries themselves are the operator's grant, not ours to remove.
================
*/
func protect(path string, directory bool) error {
	mode := privateFileMode
	if directory {
		mode = privateDirMode
	}
	extended, err := hasAccessACL(path)
	if err != nil {
		return err
	}
	if extended {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		mode |= info.Mode().Perm() & aclMaskBits
	}
	return os.Chmod(path, mode)
}
