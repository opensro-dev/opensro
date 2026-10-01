/*
===========================================================================

queue.go - native potion recovery pulses

Absolute potions credit one step immediately and queue four more. Each gauge
advances only its first potion per character timer callback. Reduction belongs
to the live keeper at credit time, not to this queue's authored amounts.

===========================================================================
*/

package recovery

const (
	potionSteps = 5
	maximumStep = 1_000_000
)

/*
================
Amount
================
*/
type Amount struct{ HP, MP int64 }

/*
================
Admission
================
*/
type Admission struct {
	Current, Maximum, Credit Amount
	Absolute                 bool
}

/*
================
pulse
================
*/
type pulse struct {
	amount    int64
	remaining uint8
}

/*
================
Queue

Owned by one authenticated character's recovery session. The zero value is
empty; discard it on death, disconnect or replacement session (4E67B0).
================
*/
type Queue struct{ hp, mp []pulse }

/*
================
Admit

49A5B0 allows HP at an exactly full promised gauge, but MP requires room.
Percentage recovery retains the first overflowing queued entry and removes
its successors; it does not resize that entry's individual pulses.
================
*/
func (q *Queue) Admit(in Admission) Amount {
	if !in.Absolute {
		q.hp = trim(q.hp, in.Current.HP, in.Maximum.HP, in.Credit.HP)
		q.mp = trim(q.mp, in.Current.MP, in.Maximum.MP, in.Credit.MP)
		return in.Credit
	}
	var out Amount
	if in.Credit.HP > 0 && in.Current.HP+promised(q.hp) <= in.Maximum.HP {
		out.HP = min(max(in.Credit.HP/potionSteps, 1), maximumStep)
		q.hp = append(q.hp, pulse{out.HP, potionSteps - 1})
	}
	if in.Credit.MP > 0 && in.Current.MP+promised(q.mp) < in.Maximum.MP {
		out.MP = min(max(in.Credit.MP/potionSteps, 1), maximumStep)
		q.mp = append(q.mp, pulse{out.MP, potionSteps - 1})
	}
	return out
}

/*
================
promised
================
*/
func promised(pulses []pulse) int64 {
	var total int64
	for _, p := range pulses {
		total += p.amount * int64(p.remaining)
	}
	return total
}

/*
================
trim
================
*/
func trim(pulses []pulse, current, maximum, credit int64) []pulse {
	if credit <= 0 {
		return pulses
	}
	excess := current + credit - maximum
	if excess > 0 {
		return nil
	}
	for i, p := range pulses {
		excess += p.amount * int64(p.remaining)
		if excess > 0 {
			return pulses[:i+1]
		}
	}
	return pulses
}

/*
================
Tick

49A510 advances at most one entry on each gauge per timer callback.
================
*/
func (q *Queue) Tick() Amount {
	var out Amount
	q.hp, out.HP = advance(q.hp)
	q.mp, out.MP = advance(q.mp)
	return out
}

/*
================
advance
================
*/
func advance(pulses []pulse) ([]pulse, int64) {
	if len(pulses) == 0 {
		return nil, 0
	}
	amount := pulses[0].amount
	pulses[0].remaining--
	if pulses[0].remaining == 0 {
		pulses = pulses[1:]
	}
	return pulses, amount
}

/*
================
Empty
================
*/
func (q *Queue) Empty() bool { return len(q.hp) == 0 && len(q.mp) == 0 }
