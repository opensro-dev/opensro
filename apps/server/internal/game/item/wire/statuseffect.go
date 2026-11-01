package wire

import "fmt"

const OpAttachedEffect uint16 = 0xB419

// AttachedEffectLayout comes from the skill record, never from optional values
// or payload length. A zero phase/rider still occupies its configured field.
type AttachedEffectLayout struct{ Status, Rider bool }
type AttachedEffect struct {
	GID, SkillID, InstanceToken uint32
	Phase                       uint8
	Rider                       uint32
}

// Client 776450 reads the status byte before the shared RPBU/STDU/DTDR word.
func (e AttachedEffect) Encode(layout AttachedEffectLayout) ([]byte, error) {
	if e.GID == 0 || e.SkillID == 0 {
		return nil, fmt.Errorf("wire: invalid attached-effect identity")
	}
	if !layout.Status && e.Phase != 2 {
		return nil, fmt.Errorf("wire: phase absent from skill layout")
	}
	if !layout.Rider && e.Rider != 0 {
		return nil, fmt.Errorf("wire: rider absent from skill layout")
	}
	w := NewWriter(17).U32(e.GID).U32(e.SkillID).U32(e.InstanceToken)
	if layout.Status {
		w.U8(e.Phase)
	}
	if layout.Rider {
		w.U32(e.Rider)
	}
	return w.Payload(), nil
}

// OpEndedEffectInstances is the v1.150 counted attached-effect teardown
// packet handled by the client's CPSMission_OnEffectTeardownTokens0xB6A0
// (client 7759B0). The v1.188 server dump's corresponding
// CharacterEffects_BroadcastEndedInstancesB072
// proves the producer lifecycle and the same [count][u32 instance ids] body;
// opcode numbers differ between those protocol revisions.
const OpEndedEffectInstances uint16 = 0xB6A0

// EndedEffectInstances is one character update's retired attached-effect
// instance list. Instance IDs, not skill IDs, address the client's live
// effect-object registry.
type EndedEffectInstances struct {
	InstanceTokens []uint32
}

func (e EndedEffectInstances) Encode() ([]byte, error) {
	if len(e.InstanceTokens) == 0 {
		return nil, fmt.Errorf("wire: ended-effect instance list is empty")
	}
	if len(e.InstanceTokens) > 0xff {
		return nil, fmt.Errorf("wire: ended-effect instance count %d exceeds u8 wire limit", len(e.InstanceTokens))
	}
	w := NewWriter(1 + 4*len(e.InstanceTokens)).U8(uint8(len(e.InstanceTokens)))
	for _, token := range e.InstanceTokens {
		w.U32(token)
	}
	return w.Payload(), nil
}

func DecodeEndedEffectInstances(payload []byte) (EndedEffectInstances, error) {
	var out EndedEffectInstances
	r := NewReader(payload)
	count, err := r.U8()
	if err != nil {
		return out, err
	}
	if count == 0 {
		return out, fmt.Errorf("wire: ended-effect instance count is zero")
	}
	out.InstanceTokens = make([]uint32, int(count))
	for index := range out.InstanceTokens {
		if out.InstanceTokens[index], err = r.U32(); err != nil {
			return EndedEffectInstances{}, err
		}
	}
	return out, r.Done()
}
