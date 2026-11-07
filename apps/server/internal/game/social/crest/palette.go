/*
===========================================================================

palette.go - the silk crest palette an upload is quantized to

The 256 RGB colours of the retail silk.dat palette (SHA-256
a15e9392b2f59d20b29227282ab7c50ccd4623d5492a832b888ee23003de75cc of
the 768 palette bytes) - the same table the client decodes .crb
payloads with (foundation/ui/guild-crest.ts), kept here so the server
can re-encode an upload into the native 16x16 palette-index payload.
The native client quantizes uploads with this palette by Manhattan
distance, last index winning a tie.

===========================================================================
*/
package crest

import "encoding/hex"

// silkPaletteHex is the palette as RRGGBB hex, one colour per index.
const silkPaletteHex = "000000800000008000808000000080800080008080808080c0dcc0a6caf02a3faa2a3fff2a5f002a5f552a5faa2a5fff2a7f002a7f552a7faa2a7fff2a9f002a9f552a9faa2a9fff2abf002abf552abfaa2abfff2adf002adf552adfaa2adfff2aff002aff552affaa2affff5500005500555500aa5500ff551f00551f55551faa551fff553f00553f55553faa553fff555f00555f55555faa555fff557f00557f55557faa557fff559f00559f55559faa559fff55bf0055bf5555bfaa55bfff55df0055df5555dfaa55dfff55ff0055ff5555ffaa55ffff7f00007f00557f00aa7f00ff7f1f007f1f557f1faa7f1fff7f3f007f3f557f3faa7f3fff7f5f007f5f557f5faa7f5fff7f7f007f7f557f7faa7f7fff7f9f007f9f557f9faa7f9fff7fbf007fbf557fbfaa7fbfff7fdf007fdf557fdfaa7fdfff7fff007fff557fffaa7fffffaa0000aa0055aa00aaaa00ffaa1f00aa1f55aa1faaaa1fffaa3f00aa3f55aa3faaaa3fffaa5f00aa5f55aa5faaaa5fffaa7f00aa7f55aa7faaaa7fffaa9f00aa9f55aa9faaaa9fffaabf00aabf55aabfaaaabfffaadf00aadf55aadfaaaadfffaaff00aaff55aaffaaaaffffd40000d40055d400aad400ffd41f00d41f55d41faad41fffd43f00d43f55d43faad43fffd45f00d45f55d45faad45fffd47f00d47f55d47faad47fffd49f00d49f55d49faad49fffd4bf00d4bf55d4bfaad4bfffd4df00d4df55d4dfaad4dfffd4ff00d4ff55d4ffaad4ffffff0055ff00aaff1f00ff1f55ff1faaff1fffff3f00ff3f55ff3faaff3fffff5f00ff5f55ff5faaff5fffff7f00ff7f55ff7faaff7fffff9f00ff9f55ff9faaff9fffffbf00ffbf55ffbfaaffbfffffdf00ffdf55ffdfaaffdfffffff55ffffaaccccffffccff33ffff66ffff99ffffccffff007f00007f55007faa007fff009f00009f55009faa009fff00bf0000bf5500bfaa00bfff00df0000df5500dfaa00dfff00ff5500ffaa2a00002a00552a00aa2a00ff2a1f002a1f552a1faa2a1fff2a3f002a3f55fffbf0a0a0a4808080ff000000ff00ffff000000ffff00ff00ffffffffff"

// silkPalette is silkPaletteHex decoded: 256 colours, 3 bytes each.
var silkPalette = decodeSilkPalette()

/*
================
decodeSilkPalette
================
*/
func decodeSilkPalette() (palette [256][3]uint8) {
	raw, err := hex.DecodeString(silkPaletteHex)
	if err != nil || len(raw) != 768 {
		panic("crest: the authored silk palette does not decode to 768 bytes")
	}
	for i := 0; i < 256; i++ {
		copy(palette[i][:], raw[i*3:])
	}
	return palette
}
