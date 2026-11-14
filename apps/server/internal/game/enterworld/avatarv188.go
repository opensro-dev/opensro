/*
===========================================================================

avatarv188.go - v1.188 avatar Lucky and 4x stacking magic options

Single owner for the v1.188 avatar magic option rows
(MATTR_AVATAR_MDIA_2..4 and MATTR_AVATAR_LUCK/_2..4) and the v1.188 dress
assignment additions, which the shipped v1.150 tables lack. Always applied
at load time to match the native v1.188 data/server.

Evidence: research finding avatar-luck-and-4x-stacking.md and v1.188
Media.pk2 server_dep/silkroad/textdata/magicoption.txt (rows 370..376) and
magicoptionassign.txt (dress row TID3 13, TID4 2).

===========================================================================
*/

package enterworld

import "fmt"

const (
	// AvatarV188OptionIDFirst and AvatarV188OptionIDLast bound the v1.188
	// avatar rows injected into magicoption.txt at load time.
	AvatarV188OptionIDFirst uint32 = 370
	AvatarV188OptionIDLast  uint32 = 376

	avatarV188TagMDIA uint32 = 1835297121 // 'mdia'
	avatarV188TagLUCK uint32 = 1819632481 // 'luca' (0x6C756361)

	avatarV188RangeWord uint32 = 1
	avatarV188Degree    int64  = 1
)

type avatarV188Spec struct {
	paramID    uint32
	optionName string
	tag        uint32
}

// Identical in shape to v1.150 id 254 MATTR_AVATAR_MDIA (range words 1,1,1,
// degree 1, descriptor "avatar_mail"); sourced from v1.188 Media.pk2
// magicoption.txt rows 370..376.
var avatarV188Specs = [...]avatarV188Spec{
	{paramID: 370, optionName: "MATTR_AVATAR_MDIA_2", tag: avatarV188TagMDIA},
	{paramID: 371, optionName: "MATTR_AVATAR_MDIA_3", tag: avatarV188TagMDIA},
	{paramID: 372, optionName: "MATTR_AVATAR_MDIA_4", tag: avatarV188TagMDIA},
	{paramID: 373, optionName: "MATTR_AVATAR_LUCK", tag: avatarV188TagLUCK},
	{paramID: 374, optionName: "MATTR_AVATAR_LUCK_2", tag: avatarV188TagLUCK},
	{paramID: 375, optionName: "MATTR_AVATAR_LUCK_3", tag: avatarV188TagLUCK},
	{paramID: 376, optionName: "MATTR_AVATAR_LUCK_4", tag: avatarV188TagLUCK},
}

var avatarV188DressOptions = [...]string{
	"MATTR_AVATAR_LUCK",
	"MATTR_AVATAR_LUCK_2",
	"MATTR_AVATAR_LUCK_3",
	"MATTR_AVATAR_LUCK_4",
	"MATTR_AVATAR_MDIA_2",
	"MATTR_AVATAR_MDIA_3",
	"MATTR_AVATAR_MDIA_4",
}

/*
================
AvatarV188OptionCollisionError
================
*/
func AvatarV188OptionCollisionError(paramID uint32) error {
	return fmt.Errorf("magicoption: v1.188 avatar param id %d already exists in base table", paramID)
}

/*
================
AvatarV188MagicOptions

Returns fresh MagicOptionRow copies for the seven v1.188 avatar rows
(ids 370..376) absent from v1.150 magicoption.txt, so callers cannot alias
shared slices.
================
*/
func AvatarV188MagicOptions() []MagicOptionRow {
	rows := make([]MagicOptionRow, 0, len(avatarV188Specs))
	for _, spec := range avatarV188Specs {
		words := [3]uint32{avatarV188RangeWord, avatarV188RangeWord, avatarV188RangeWord}
		rows = append(rows, MagicOptionRow{
			RangeWords:  &words,
			ParamID:     spec.paramID,
			OptionName:  spec.optionName,
			ParamName:   "+",
			Degree:      avatarV188Degree,
			Tag:         spec.tag,
			Descriptors: []string{"avatar_mail"},
		})
	}
	return rows
}

/*
================
AvatarV188DressOptions

Returns the seven v1.188 avatar dress option codenames appended to the
v1.150 magicoptionassign.txt dress row (TID3 13, TID4 2).
================
*/
func AvatarV188DressOptions() []string {
	out := make([]string, len(avatarV188DressOptions))
	copy(out, avatarV188DressOptions[:])
	return out
}
