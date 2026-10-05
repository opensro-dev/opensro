package domain

const (
	// BeginnerMarkMaxLevel is the native CIFOption game-page gate
	// (sub_5cb550: CICPlayer_GetLevel() <= 0x13).
	BeginnerMarkMaxLevel int64 = 19
	VisualFlagBeginner   uint8 = 1 << 0
	// VisualFlagHelper is the Helper status mark: CGInterface_ExecuteActionCommand
	// (695420) action 1011 toggles +0x779 bit 1 and sends it in 0x7683, and
	// the 0x8000001E state effect draws it over the character.
	VisualFlagHelper     uint8 = 1 << 1
	VisualFlagsKnownMask uint8 = VisualFlagBeginner | VisualFlagHelper
)

// ResolveVisualFlags supplies the retail creation fallback for records that
// predate persisted visual flags: the beginner mark starts enabled through
// level 19. Once a value has been persisted, it is authoritative.
func ResolveVisualFlags(c *Character) uint8 {
	if c == nil {
		return 0
	}
	if c.VisualFlags != nil {
		return uint8(*c.VisualFlags) & VisualFlagsKnownMask
	}
	level := int64(1)
	if c.Level != nil {
		level = *c.Level
	}
	if level <= BeginnerMarkMaxLevel {
		return VisualFlagBeginner
	}
	return 0
}
