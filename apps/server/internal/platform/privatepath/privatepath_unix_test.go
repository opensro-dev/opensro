//go:build !windows

/*
===========================================================================

privatepath_unix_test.go - ACL mask repair coverage

The byte-level cases pin repairACLMask's union rule; the protect case
drives a real directory through the boot-time tightening and reads the
grant back (issue #2860).

===========================================================================
*/

package privatepath

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

/*
================
aclEntryBytes

One 8-byte entry of the binary ACL encoding repairACLMask reads.
================
*/
func aclEntryBytes(tag uint16, perm uint16, qualifier uint32) []byte {
	entry := make([]byte, aclEntrySize)
	binary.LittleEndian.PutUint16(entry[0:], tag)
	binary.LittleEndian.PutUint16(entry[2:], perm)
	binary.LittleEndian.PutUint32(entry[4:], qualifier)
	return entry
}

/*
================
aclBytes

A whole version-2 access ACL from its entries.
================
*/
func aclBytes(entries ...[]byte) []byte {
	raw := make([]byte, 4)
	binary.LittleEndian.PutUint32(raw, aclVersion)
	for _, entry := range entries {
		raw = append(raw, entry...)
	}
	return raw
}

/*
================
aclMaskPerm

The mask entry's permissions in a binary ACL, or -1 when there is none.
================
*/
func aclMaskPerm(raw []byte) int {
	body := raw[4:]
	for at := 0; at+aclEntrySize <= len(body); at += aclEntrySize {
		if binary.LittleEndian.Uint16(body[at:]) == aclTagMask {
			return int(binary.LittleEndian.Uint16(body[at+2:]))
		}
	}
	return -1
}

/*
================
TestRepairACLMaskRestoresTheNamedGrant

The boot case from issue #2860: the authority at 0700 with a named
traverse grant, after chmod left the mask at zero. The mask comes back
as the named entry's reach; every other entry is byte-identical.
================
*/
func TestRepairACLMaskRestoresTheNamedGrant(t *testing.T) {
	raw := aclBytes(
		aclEntryBytes(aclTagUserObj, 0o7, aclUndefinedQualifier),
		aclEntryBytes(aclTagUser, 0o1, 1001),
		aclEntryBytes(aclTagGroupObj, 0, aclUndefinedQualifier),
		aclEntryBytes(aclTagMask, 0, aclUndefinedQualifier),
		aclEntryBytes(aclTagOther, 0, aclUndefinedQualifier),
	)
	repaired, changed := repairACLMask(raw)
	if !changed {
		t.Fatal("repairACLMask reported no change for a zeroed mask with a named entry")
	}
	want := aclBytes(
		aclEntryBytes(aclTagUserObj, 0o7, aclUndefinedQualifier),
		aclEntryBytes(aclTagUser, 0o1, 1001),
		aclEntryBytes(aclTagGroupObj, 0, aclUndefinedQualifier),
		aclEntryBytes(aclTagMask, 0o1, aclUndefinedQualifier),
		aclEntryBytes(aclTagOther, 0, aclUndefinedQualifier),
	)
	if !bytes.Equal(repaired, want) {
		t.Fatal("repair changed bytes beyond the mask entry's permissions")
	}
	if got := aclMaskPerm(raw); got != 0 {
		t.Fatal("repair mutated its input")
	}
}

/*
================
TestRepairACLMaskUnionsTheGroupAndNamedEntries

setfacl's own mask rule: the owning group's permissions union every
named entry, user and group alike.
================
*/
func TestRepairACLMaskUnionsTheGroupAndNamedEntries(t *testing.T) {
	raw := aclBytes(
		aclEntryBytes(aclTagUserObj, 0o7, aclUndefinedQualifier),
		aclEntryBytes(aclTagUser, 0o4, 1001),
		aclEntryBytes(aclTagGroupObj, 0o5, aclUndefinedQualifier),
		aclEntryBytes(aclTagGroup, 0o2, 2002),
		aclEntryBytes(aclTagMask, 0o4, aclUndefinedQualifier),
		aclEntryBytes(aclTagOther, 0, aclUndefinedQualifier),
	)
	repaired, changed := repairACLMask(raw)
	if !changed {
		t.Fatal("repairACLMask reported no change for a mask below the union")
	}
	if got := aclMaskPerm(repaired); got != 0o7 {
		t.Fatalf("mask after repair = %o, want 7 (r-x | r-- | -w-)", got)
	}
}

/*
================
TestRepairACLMaskLeavesSoundACLsAlone

No named entries, an already-correct mask, and bytes that are not a
provable version-2 ACL all come back untouched.
================
*/
func TestRepairACLMaskLeavesSoundACLsAlone(t *testing.T) {
	plain := aclBytes(
		aclEntryBytes(aclTagUserObj, 0o7, aclUndefinedQualifier),
		aclEntryBytes(aclTagGroupObj, 0, aclUndefinedQualifier),
		aclEntryBytes(aclTagOther, 0, aclUndefinedQualifier),
	)
	if _, changed := repairACLMask(plain); changed {
		t.Fatal("an ACL without named entries was changed")
	}

	sound := aclBytes(
		aclEntryBytes(aclTagUserObj, 0o7, aclUndefinedQualifier),
		aclEntryBytes(aclTagUser, 0o1, 1001),
		aclEntryBytes(aclTagGroupObj, 0, aclUndefinedQualifier),
		aclEntryBytes(aclTagMask, 0o1, aclUndefinedQualifier),
		aclEntryBytes(aclTagOther, 0, aclUndefinedQualifier),
	)
	if _, changed := repairACLMask(sound); changed {
		t.Fatal("an ACL whose mask already equals the union was changed")
	}

	for _, raw := range [][]byte{
		nil,
		{1, 2, 3},
		{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{2, 0, 0, 0, 1, 0},
	} {
		if _, changed := repairACLMask(raw); changed {
			t.Fatalf("unprovable ACL bytes %v were changed", raw)
		}
	}
}

/*
================
TestProtectRestoresTheGrantMask

End to end on a real directory: an operator's named traverse grant is
on the directory, protect runs the boot-time 0700 tightening, and the
grant's mask must survive at its named reach with the mode still
closed to everyone else. Fails before the repair existed.
================
*/
func TestProtectRestoresTheGrantMask(t *testing.T) {
	dir := t.TempDir()
	acl := aclBytes(
		aclEntryBytes(aclTagUserObj, 0o7, aclUndefinedQualifier),
		aclEntryBytes(aclTagUser, 0o1, uint32(os.Getuid())),
		aclEntryBytes(aclTagGroupObj, 0, aclUndefinedQualifier),
		aclEntryBytes(aclTagMask, 0o1, aclUndefinedQualifier),
		aclEntryBytes(aclTagOther, 0, aclUndefinedQualifier),
	)
	if err := unix.Setxattr(dir, aclAccessXattr, acl, 0); err != nil {
		if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENODATA) {
			t.Skipf("filesystem does not accept POSIX ACLs: %v", err)
		}
		t.Fatalf("installing the operator grant: %v", err)
	}
	if err := protect(dir, true); err != nil {
		t.Fatalf("protect: %v", err)
	}
	size, err := unix.Getxattr(dir, aclAccessXattr, nil)
	if err != nil {
		t.Fatalf("reading the ACL back: %v", err)
	}
	raw := make([]byte, size)
	if _, err := unix.Getxattr(dir, aclAccessXattr, raw); err != nil {
		t.Fatalf("reading the ACL back: %v", err)
	}
	if got := aclMaskPerm(raw); got != 0o1 {
		t.Fatalf("mask after protect = %o, want 1 (the grant's --x survives the 0700 chmod)", got)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o007 != 0 || perm&0o700 != 0o700 {
		t.Fatalf("mode after protect = %o, want owner-only access outside the ACL mask", perm)
	}
}
