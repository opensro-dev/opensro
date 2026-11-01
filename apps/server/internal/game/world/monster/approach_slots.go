package monster

import "math"

// ApproachSlots is the reference server squad's target-local occupancy
// array (its per-target assignment probe and next-candidate search).
// A remembered direction is not always a reservation; eight slots are NOT an
// attack cap and are not solid-body collision. The population owns all writes.
type ApproachSlots [8]uint32

func (s *ApproachSlots) Release(gid uint32) {
	for i, owner := range s {
		if owner == gid {
			s[i] = 0
		}
	}
}

// Assign preserves alternating search and moving-incumbent preemption.
// mayPreempt must test a STRICTLY closer candidate against a moving incumbent.
// When full, return the preferred direction without replacing its owner.
func (s *ApproachSlots) Assign(gid uint32, preferred int, mayPreempt func(uint32) bool) int {
	if gid == 0 || preferred < 0 || preferred >= len(s) {
		return -1
	}
	count := 0
	for _, owner := range s {
		if owner != 0 {
			count++
		}
	}
	if count == len(s) {
		return preferred
	}
	if s[preferred] == 0 {
		s[preferred] = gid
		return preferred
	}
	for step := 1; step <= len(s); step++ {
		plus := (preferred + step) % len(s)
		if s[plus] == 0 {
			s[plus] = gid
			return plus
		}
		minus := (preferred - step + len(s)) % len(s)
		if s[minus] == 0 || mayPreempt != nil && mayPreempt(s[minus]) {
			s[minus] = gid
			return minus
		}
	}
	return -1
}

// The reference server's preferred-slot computation divides an unsigned
// angle in degrees by 180.0, then truncates.
// This surprising divisor is present in the supplied machine code; replacing
// it with atan2/octants would be a new policy, not a transcription.
func NativeApproachPreferredSlot(actor, target Pose) int {
	x, _, z := NativeTacticsRelative(target, actor)
	length := float32(math.Sqrt(float64(float32(float64(x)*float64(x) + float64(z)*float64(z)))))
	if !(length > 0) {
		return 0
	}
	dot := float32(x / length)
	if dot < -1 {
		dot = -1
	}
	if dot > 1 {
		dot = 1
	}
	angle := float32(math.Acos(float64(dot)))
	degrees := float32(float64(angle) * 57.295780181884766)
	slot := int(float64(degrees) / 180.0)
	if slot < 0 {
		return 0
	}
	if slot > 7 {
		return 7
	}
	return slot
}

// The reference server's direction-table builder constructs normalized
// directions at 22+45*i degrees (integer 0x16, not 22.5). Its offset
// destination initially scales them by 0.8 of effective action reach.
func NativeApproachOffset(slot int, effectiveReach float32) (x, z float32) {
	if slot < 0 || slot >= 8 || !(effectiveReach > 0) {
		return 0, 0
	}
	angle := float32(float64(22+45*slot) * 0.01745329238474369)
	x, z = float32(math.Cos(float64(angle))), -float32(math.Sin(float64(angle)))
	length := float32(math.Sqrt(float64(float32(float64(x)*float64(x) + float64(z)*float64(z)))))
	x, z = float32(x/length), float32(z/length)
	radius := float32(float64(effectiveReach) * 0.80000001192092896)
	return float32(x * radius), float32(z * radius)
}
