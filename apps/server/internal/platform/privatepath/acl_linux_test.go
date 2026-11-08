/*
===========================================================================

acl_linux_test.go - ProtectDirectory against a real POSIX access ACL

An operator grants a named user read and search on a private directory
(as ops does for the stall-dump watch). A server start must still force
0700 owner/other bits, but must not zero the ACL mask that keeps the
named entry effective (#2860).

===========================================================================
*/

package privatepath

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// Linux posix_acl_xattr layout: a u32 version header, then entries of
// u16 tag, u16 perm, u32 id, sorted by tag (include/uapi/linux/posix_acl_xattr.h).
const (
	aclXattrVersion = 2
	aclTagUserObj   = 0x01
	aclTagUser      = 0x02
	aclTagGroupObj  = 0x04
	aclTagMask      = 0x10
	aclTagOther     = 0x20
	aclUndefinedID  = 0xffffffff
	aclPermRX       = 0o5
	aclEntrySize    = 8
	aclHeaderSize   = 4
	// grantedUID is any uid other than the test's own; the owner may name it.
	grantedUIDOffset = 1
)

type aclEntry struct {
	tag  uint16
	perm uint16
	id   uint32
}

/*
================
encodeACL
================
*/
func encodeACL(entries []aclEntry) []byte {
	out := make([]byte, aclHeaderSize+len(entries)*aclEntrySize)
	binary.LittleEndian.PutUint32(out, aclXattrVersion)
	for i, entry := range entries {
		at := aclHeaderSize + i*aclEntrySize
		binary.LittleEndian.PutUint16(out[at:], entry.tag)
		binary.LittleEndian.PutUint16(out[at+2:], entry.perm)
		binary.LittleEndian.PutUint32(out[at+4:], entry.id)
	}
	return out
}

/*
================
readMaskPerm

Returns the mask entry's permission bits from path's access ACL.
================
*/
func readMaskPerm(t *testing.T, path string) uint16 {
	t.Helper()
	buf := make([]byte, 256)
	n, err := unix.Getxattr(path, accessACLAttr, buf)
	if err != nil {
		t.Fatalf("read ACL: %v", err)
	}
	for at := aclHeaderSize; at+aclEntrySize <= n; at += aclEntrySize {
		if binary.LittleEndian.Uint16(buf[at:]) == aclTagMask {
			return binary.LittleEndian.Uint16(buf[at+2:])
		}
	}
	t.Fatalf("ACL has no mask entry")
	return 0
}

/*
================
grantNamedUser

Gives a named user r-x on dir through an access ACL, as setfacl -m
u:<uid>:rx would. Skips when the file system has no ACL support.
================
*/
func grantNamedUser(t *testing.T, dir string) {
	t.Helper()
	acl := encodeACL([]aclEntry{
		{tag: aclTagUserObj, perm: 0o7, id: aclUndefinedID},
		{tag: aclTagUser, perm: aclPermRX, id: uint32(os.Getuid() + grantedUIDOffset)},
		{tag: aclTagGroupObj, perm: 0, id: aclUndefinedID},
		{tag: aclTagMask, perm: aclPermRX, id: aclUndefinedID},
		{tag: aclTagOther, perm: 0, id: aclUndefinedID},
	})
	if err := unix.Setxattr(dir, accessACLAttr, acl, 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) {
			t.Skip("file system has no POSIX ACL support")
		}
		t.Fatalf("set ACL: %v", err)
	}
}

/*
================
TestProtectDirectoryKeepsNamedACLEffective
================
*/
func TestProtectDirectoryKeepsNamedACLEffective(t *testing.T) {
	dir := t.TempDir()
	grantNamedUser(t, dir)
	// Widen other bits so the call has something to tighten.
	if err := os.Chmod(dir, 0o757); err != nil {
		t.Fatal(err)
	}

	if err := ProtectDirectory(dir); err != nil {
		t.Fatalf("ProtectDirectory: %v", err)
	}

	if got := readMaskPerm(t, dir); got != aclPermRX {
		t.Fatalf("ACL mask = %o, want %o (named entry disabled)", got, aclPermRX)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("mode = %o, want 750 (owner rwx, mask r-x, other none)", got)
	}
}

/*
================
TestProtectDirectoryWithoutACLIsExactly0700
================
*/
func TestProtectDirectoryWithoutACLIsExactly0700(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ProtectDirectory(dir); err != nil {
		t.Fatalf("ProtectDirectory: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("mode = %o, want 700", got)
	}
}
