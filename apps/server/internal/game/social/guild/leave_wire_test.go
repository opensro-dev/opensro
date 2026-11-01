package guild

import (
	"testing"
)

// TestDecodeLeaveRequest pins the 0x756E strict decoder: exactly one
// u32 (the NetClient_SendSelectedTarget756E composer writes 4 bytes from CGInterface+0x620)
// and nothing else - short, empty and trailing-byte payloads error.
func TestDecodeLeaveRequest(t *testing.T) {
	t.Parallel()
	ok, err := DecodeLeaveRequest(invitePayload(0x00C40007))
	if err != nil {
		t.Fatalf("well-formed leave decode error: %v", err)
	}
	if ok.SelectedTargetGid != 0x00C40007 {
		t.Errorf("decoded gid = %#x, want 0x%X", ok.SelectedTargetGid, 0x00C40007)
	}

	// A ZERO gid is well-formed: the +0x620 slot reads 0 with no
	// selected target, which is the PROBABLE-common leave case.
	zero, err := DecodeLeaveRequest(invitePayload(0))
	if err != nil {
		t.Fatalf("zero-gid leave decode error (must be well-formed): %v", err)
	}
	if zero.SelectedTargetGid != 0 {
		t.Errorf("zero-gid decoded = %+v", zero)
	}

	for name, malformed := range map[string][]byte{
		"nil":      nil,
		"empty":    {},
		"short":    {0x07, 0x00, 0xC4},
		"trailing": append(invitePayload(7), 0x00),
	} {
		if _, err := DecodeLeaveRequest(malformed); err == nil {
			t.Errorf("%s: malformed leave body % X decoded without error", name, malformed)
		}
	}
}
