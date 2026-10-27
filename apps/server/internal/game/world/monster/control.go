package monster

import "fmt"

// CSNM 11's payload byte, independent of the actor's TID and spawn origin.
// The reference server's control binding selects the tactics virtuals
// 48/4C/50; CSNM 12 restores virtual 44.
type ControlMode uint8

const (
	ControlNone ControlMode = iota
	ControlOwned
	ControlSummoned
	ControlPassive
)

type actorControl struct {
	gid  uint32
	mode ControlMode
}

func (m MoverState) ControllerGID() uint32    { return m.control.gid }
func (m MoverState) ControlMode() ControlMode { return m.control.mode }

// BindController applies the AI state part of the reference server's
// control binding. The caller must resolve
// the controller and admit its native LIFE==1 before committing this value.
// Hive detachment for mode 3 belongs to the population transaction.
func (m *MoverState) BindController(mode ControlMode, gid uint32) error {
	if gid == 0 || mode < ControlOwned || mode > ControlPassive {
		return fmt.Errorf("invalid control binding: mode=%d gid=%d", mode, gid)
	}
	next := *m
	if err := next.Transition(MoverEventControlBound, 0); err != nil {
		return err
	}
	next.control = actorControl{gid: gid, mode: mode}
	*m = next
	return nil
}

func (m *MoverState) ReleaseController(gid uint32) (bool, error) {
	if gid == 0 || m.control.gid != gid {
		return false, nil
	}
	next := *m
	if err := next.Transition(MoverEventControlReleased, 0); err != nil {
		return false, err
	}
	next.control = actorControl{}
	*m = next
	return true, nil
}

// The reference server's wander entry checks run capability first and
// refuses non-summoned COS wander. The classification reads the packed TID,
// not the C++ allocation subtype.
func CanEnterWander(tid uint16, runSpeed float64) bool {
	return runSpeed > 0 && (tid&0x7fe != 0x1c6 || tid&0xf800 == 0x2800)
}
