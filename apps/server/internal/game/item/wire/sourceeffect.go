package wire

import "fmt"

// Client 776600 is registered as B5ED in v1.150. The research server's
// analogous packet uses a different opcode. This packet is private to the
// source character; GID names the subject, not the effect's owning character.
const OpSourceEffect uint16 = 0xB5ED

type SourceEffect struct {
	SkillID, InstanceToken, SubjectGID uint32
	// Already encoded in the session's native byte-string encoding. Keeping
	// bytes here prevents UTF-8 rune counts from becoming native byte lengths.
	SubjectName []byte
	Rider       uint32
}

// Only RPBU/STDU add a rider here. B419's broader effect-rider predicate
// (which also includes DTDR) must not be reused for this layout.
func (e SourceEffect) Encode(stealthDuration bool) ([]byte, error) {
	if e.SkillID == 0 || len(e.SubjectName) > 0xffff {
		return nil, fmt.Errorf("wire: invalid source effect")
	}
	if !stealthDuration && e.Rider != 0 {
		return nil, fmt.Errorf("wire: source effect rider absent from skill layout")
	}
	w := NewWriter(18 + len(e.SubjectName)).U32(e.SkillID).U32(e.InstanceToken).
		U32(e.SubjectGID).U16(uint16(len(e.SubjectName))).Bytes(e.SubjectName)
	if stealthDuration {
		w.U32(e.Rider)
	}
	return w.Payload(), nil
}
