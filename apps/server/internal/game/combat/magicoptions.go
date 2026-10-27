/*
===========================================================================

magicoptions.go - item magic options and their parameter writes

===========================================================================
*/

package combat

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

// magicOption is one decoded item option (the server's option decoder): the definition tag and
// the high-dword magnitude.
type magicOption struct {
	tag   uint32
	value uint32
}

func optionTag(code string) uint32 {
	var tag uint32
	for i := 0; i < len(code); i++ {
		tag = tag<<8 | uint32(code[i])
	}
	return tag
}

/*
==================
resolveMagicOptions

resolveMagicOptions decodes the wire params: low u16 selects the
magicoption.txt row, the high dword is the magnitude. An unresolved row is
the native MiniDump path and refuses the projection.
==================
*/
func resolveMagicOptions(codename string, encoded []uint64, source enterworld.MagicOptionSource) ([]magicOption, error) {
	if len(encoded) == 0 {
		return nil, nil
	}
	if len(encoded) > 12 {
		return nil, fmt.Errorf("combat: item %q carries %d magic options (native limit 12)", codename, len(encoded))
	}
	if source == nil {
		return nil, fmt.Errorf("combat: item %q has magic options but no magic-option source is wired", codename)
	}
	options := make([]magicOption, 0, len(encoded))
	for _, v := range encoded {
		row, ok := source.MagicOptionByParamID(uint32(v & 0xffff))
		if !ok || row == nil || row.Tag == 0 {
			return nil, fmt.Errorf("combat: item %q magic option %d has no magicoption.txt row", codename, v&0xffff)
		}
		options = append(options, magicOption{tag: row.Tag, value: uint32(v >> 32)})
	}
	return options, nil
}

/*
==================
applyItemMagicOptions

applyItemMagicOptions is the item-level option pass's stat half: 'hr' and 'er' raise the item's
own hit (+1D8) and evasion (+19C) ratings by a percentage, in x87 and
stored to float32. The durability half ('dura', 'duru', 'nrep') moves the
item's maximum durability, which the durability owner projects.
==================
*/
func applyItemMagicOptions(item *Stats, options []magicOption) {
	for _, o := range options {
		switch o.tag {
		case optionTag("hr"):
			item.HitRate = float64(float32(float64(o.value)/100*item.HitRate + item.HitRate))
		case optionTag("er"):
			item.EvasionRate = float64(float32(float64(o.value)/100*item.EvasionRate + item.EvasionRate))
		}
	}
}

/*
==================
magicOptionWrites

magicOptionWrites is the server's per-item option-to-keeper pass for one equipped item: each option's
keeper writes, keyed by the item as the native source. Tags the v1.150
magicoption.txt does not ship are the native MiniDump path.
==================
*/
func magicOptionWrites(codename string, options []magicOption, source uint32) ([]paramkeeper.Write, error) {
	var writes []paramkeeper.Write
	// The server reads the item's own current contribution to one parameter;
	// the avatar options accumulate onto it.
	own := map[uint16]float32{}
	flat := func(param uint16, value float32) {
		own[param] = value
		writes = append(writes, paramkeeper.Write{Parameter: param, Channel: paramkeeper.Flat, Source: source, Value: value})
	}
	add := func(param uint16, channel paramkeeper.Channel, value float32) {
		writes = append(writes, paramkeeper.Write{Parameter: param, Channel: channel, Source: source, Value: value})
	}
	for _, o := range options {
		v := float32(o.value)
		switch o.tag {
		case optionTag("str"), optionTag("strj"), optionTag("strv"):
			flat(1, v)
		case optionTag("int"), optionTag("intj"), optionTag("intv"):
			flat(2, v)
		case optionTag("stra"):
			flat(1, float32(int32(own[1])+int32(o.value)))
		case optionTag("inta"):
			flat(2, float32(int32(own[2])+int32(o.value)))
		case optionTag("mdia"):
			flat(0xbc, float32(int32(own[0xbc])+int32(o.value)))
		case optionTag("hp"):
			// Accumulates onto the item's own contribution like hpa
			// below: the keeper replaces per source, so a plain write
			// after an hpa write would silently drop its magnitude.
			flat(3, float32(int32(own[3])+int32(o.value)))
		case optionTag("mp"):
			flat(4, float32(int32(own[4])+int32(o.value)))
		case optionTag("fz"):
			// Frostbite resistance covers freezing and frostbite.
			flat(0x1b, v)
			flat(0x1c, v)
		case optionTag("es"):
			flat(0x1d, v)
		case optionTag("bu"):
			flat(0x1e, v)
		case optionTag("ps"):
			flat(0x1f, v)
		case optionTag("zb"):
			flat(0x20, v)
		case optionTag("evbl"):
			flat(0x38, v)
		case optionTag("evcr"):
			flat(0x39, v)
		case optionTag("hra"):
			add(0xb, paramkeeper.PercentSum, v)
		case optionTag("era"):
			add(9, paramkeeper.PercentSum, v)
		case optionTag("hpa"):
			// The avatar HP row is a flat increase: its client text is
			// "PARAM_HP 150" with no percent sign (the rate rows, hra and
			// era, carry one), and its rolled range is a fixed 150. It
			// lands on the max-HP node like the equipment hp option.
			flat(3, float32(int32(own[3])+int32(o.value)))
		case optionTag("mpa"):
			flat(4, float32(int32(own[4])+int32(o.value)))
		case optionTag("hprg"):
			add(0x19, paramkeeper.PercentSum, v)
		case optionTag("mprg"):
			add(0x1a, paramkeeper.PercentSum, v)
		case optionTag("drua"):
			for param := uint16(0x80); param <= 0x83; param++ {
				flat(param, v)
			}
		case optionTag("dara"):
			for param := uint16(0xae); param <= 0xb1; param++ {
				add(param, paramkeeper.PercentProduct, -v)
			}
		case optionTag("hr"), optionTag("er"), optionTag("dura"), optionTag("duru"), optionTag("nrep"),
			optionTag("rein"), optionTag("atha"), optionTag("soli"), optionTag("luck"), optionTag("astr"),
			optionTag("rep"):
			// Item-level or presentation-only options: no keeper write.
		default:
			return nil, fmt.Errorf("combat: item %q magic option tag %#x has no v1.150 stat semantics", codename, o.tag)
		}
	}
	return writes, nil
}
