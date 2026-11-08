//go:build !windows

/*
===========================================================================

privatepath_unix.go - Unix permissions for service-private paths

protect tightens a path to the service identity with chmod. On Linux a
chmod also rewrites the ACL mask from the new mode's group bits, which
silently disables any named ACL grant an operator put on the path;
restoreACLMask puts the mask back (issue #2860).

===========================================================================
*/

package privatepath

import (
	"encoding/binary"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

const aclAccessXattr = "system.posix_acl_access"

// The system.posix_acl_access encoding: a little-endian version word,
// then 8-byte entries of tag, permissions and qualifier.
const (
	aclVersion            = 2
	aclEntrySize          = 8
	aclTagUserObj         = 0x01
	aclTagUser            = 0x02
	aclTagGroupObj        = 0x04
	aclTagGroup           = 0x08
	aclTagMask            = 0x10
	aclTagOther           = 0x20
	aclUndefinedQualifier = 0xffffffff
)

/*
================
protect

Tightens a file to 0600 or a directory to 0700, then restores the ACL
mask the chmod rewrote (restoreACLMask). The chmod is never skipped:
the mode always ends exactly at the target.
================
*/
func protect(path string, directory bool) error {
	mode := os.FileMode(0o600)
	if directory {
		mode = 0o700
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	return restoreACLMask(path)
}

/*
================
restoreACLMask

chmod(2) sets the ACL mask from the new mode's group bits, so the
tightening chmod in protect zeroes the mask of any ACL carrying named
entries and disables an operator's grant while the entries still read
as present - the stall-dump watch lost its authority traverse this way
(issue #2860). When the access ACL holds named user or group entries,
the mask is re-asserted as the union of the owning group's and the
named entries' permissions, the mask setfacl itself computes: the
grant survives with exactly the reach its entries name, and everyone
else keeps the tightened mode. A path with no ACL, or a filesystem
without ACL support, keeps the plain chmod result.
================
*/
func restoreACLMask(path string) error {
	size, err := unix.Getxattr(path, aclAccessXattr, nil)
	if err != nil {
		if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.EOPNOTSUPP) {
			return nil
		}
		return err
	}
	raw := make([]byte, size)
	if _, err := unix.Getxattr(path, aclAccessXattr, raw); err != nil {
		return err
	}
	repaired, changed := repairACLMask(raw)
	if !changed {
		return nil
	}
	return unix.Setxattr(path, aclAccessXattr, repaired, 0)
}

/*
================
repairACLMask

Recomputes the mask entry of a binary POSIX access ACL as the union of
the owning group's and every named entry's permissions, and returns
the ACL with that mask written. The input comes back untouched when it
holds no named entry, has no mask entry, or is not a version-2 ACL this
function can prove out. The union grants nothing the named entries do
not already name; the owning group stays at its post-chmod reach
because its entry is part of the union.
================
*/
func repairACLMask(raw []byte) ([]byte, bool) {
	if len(raw) < 4 || binary.LittleEndian.Uint32(raw) != aclVersion {
		return raw, false
	}
	body := raw[4:]
	if len(body)%aclEntrySize != 0 {
		return raw, false
	}
	maskAt := -1
	mask := uint16(0)
	named := false
	for at := 0; at < len(body); at += aclEntrySize {
		tag := binary.LittleEndian.Uint16(body[at:])
		perm := binary.LittleEndian.Uint16(body[at+2:])
		switch tag {
		case aclTagGroupObj:
			mask |= perm
		case aclTagUser, aclTagGroup:
			mask |= perm
			named = true
		case aclTagMask:
			maskAt = at
		}
	}
	if !named || maskAt < 0 {
		return raw, false
	}
	if binary.LittleEndian.Uint16(body[maskAt+2:]) == mask {
		return raw, false
	}
	repaired := make([]byte, len(raw))
	copy(repaired, raw)
	binary.LittleEndian.PutUint16(repaired[4+maskAt+2:], mask)
	return repaired, true
}
