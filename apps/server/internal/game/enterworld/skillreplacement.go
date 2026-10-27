package enterworld

import (
	"fmt"

	"opensro.online/server/internal/game/item/statuseffect"
)

/*
================
replacementMetadataWord
================
*/
func replacementMetadataWord(fields []string, column int) (uint32, error) {
	value, ok := textdataInt(fields[column])
	if !ok || value < 0 || value > 0xffffffff {
		return 0, fmt.Errorf("replacement metadata: invalid word at column %d", column)
	}

	return uint32(value), nil
}

/*
================
compileSkillReplacement

Builds replacement metadata. Does not admit the skill for execution.

Decode the complete v1.150 program first. Argument words can look like
opcodes. The server's strides do not describe this client.

Native behaviour, in words: the server's parameter indexer, its selector
install, its replacement decision and its state-conflict check.
================
*/
func compileSkillReplacement(fields []string) (statuseffect.ReplacementDescriptor, error) {
	var (
		d     statuseffect.ReplacementDescriptor
		zero  statuseffect.ReplacementDescriptor
		value uint32
		abnb  bool
	)

	p, err := CompileSkillProgram(fields)
	if err != nil {
		return zero, err
	}

	// read the replacement header
	value, err = replacementMetadataWord(fields, 8)
	if err != nil || value > 255 {
		return zero, fmt.Errorf("invalid skill activity")
	}
	d.Activity = uint8(value)

	d.Category, err = replacementMetadataWord(fields, 68)
	if err != nil {
		return zero, err
	}

	d.Group, err = replacementMetadataWord(fields, 2)
	if err != nil {
		return zero, err
	}

	value, err = replacementMetadataWord(fields, 7)
	if err != nil || value > 255 {
		return zero, fmt.Errorf("replacement metadata: invalid rank at column 7")
	}
	d.Rank = uint8(value)

	d.BasicCode = fields[5]
	if d.BasicCode == "" {
		return zero, fmt.Errorf("replacement metadata: empty basic code at column 5")
	}

	d.PackedStates, err = replacementMetadataWord(fields, 18)
	if err != nil {
		return zero, err
	}

	// walk decoded instructions, not raw argument words
	for _, inst := range p.instructions {
		switch inst.Tag {
		case 0x736b63: // skc
			// event retirement, independent of voluntary nbuf
			d.EventCancelMask = uint8(inst.Arguments[1])

		case 0x6d736368: // msch
			// +4A4: the last block wins
			d.MschPresent = true
			d.MschMode = inst.Arguments[0]

		case 0x63627566: // cbuf
			// +358: presence counts even without a payload
			d.Cbuf = true

		case 0x64747470: // dttp
			// +424
			d.DttpPresent = true
			d.DttpKind = inst.Arguments[0]
			d.DttpRank = inst.Arguments[1]

		case 0x656672: // efr
			// discriminator 2 uses +290; other variants have separate slots
			d.Efr2 = d.Efr2 || inst.Arguments[0] == 2

		case 0x6c6e6b73: // lnks
			d.Lnks = true

		case 0x6c6b7332: // lks2
			d.Lks2 = true

		case 0x6f766c32: // ovl2
			d.Ovl2Present = true
			d.Ovl2 = inst.Arguments[0]

		case 0x61626e62: // abnb
			// +41C: overrides the complete selector
			abnb = true
		}

		if replacementExecutionInstruction(inst.Tag) {
			d.MatchesExecutionSelector = true
		}
	}

	// apply the override after all blocks have been read
	if abnb {
		d.MatchesExecutionSelector = false
	}

	return d, nil
}

/*
================
replacementExecutionInstruction

The 36 pointer slots tested by the server's selector, resolved through its parameter index.
Only presence matters. Argument values do not affect this test.

Tests use the original native indexer and selector execution for
expected results, not this table.
================
*/
func replacementExecutionInstruction(tag uint32) bool {
	switch tag {
	case 0x617474, 0x667a, 0x6662, 0x6573, 0x6275, 0x7073, 0x7a62,
		0x7365, 0x7274, 0x736c, 0x6665, 0x6d79, 0x626c, 0x646e,
		0x7374, 0x6473, 0x6361, 0x63737372, 0x63736974, 0x63737064,
		0x63736d64, 0x63736870, 0x63736d70, 0x6c667374, 0x7462,
		0x636b, 0x70646d67, 0x70646d32, 0x74616e74, 0x746e7432,
		0x64747470, 0x74657264, 0x6869746d, 0x74726170, 0x686e7470, 0x74687264:
		return true
	}

	return false
}
