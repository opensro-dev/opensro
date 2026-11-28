/*
===========================================================================

spec.go - the [BIIV] prize property spec of a later gachaitemset row

A v1.696 (and VSRO 1.188) gachaitemset row carries up to four (type,
string) pairs; type 1 is a property spec such as
"[BIIV]<M:str,1,3><M:int,1,3><O:3>". The v1.188 server parses it in its
free-param reader: it finds the "[BIIV]" marker, then turns each
"<...>" token into a 16-byte record (valid byte, letter, the M level
byte, the M option tag, an i64 value). At most five tokens; a repeated
letter, or a repeated M option, asserts. The row loader then checks each
token against the reward item: O and M and D need equipment, O clamps to
0..12, M names a magic option whose value range holds the value, D clamps
to the durability range.

The exchange turns the tokens into the override struct that the
special-equipment property roll reads: O fixes the plus level, V fixes
the 64-bit white variance word (durability follows its first field), D
fixes durability, and M replaces the random blue options with exactly the
listed ones. Evidence:
research/findings/magic-pop-biiv-server-v1188.md.

===========================================================================
*/

package gacha

import (
	"fmt"
	"strconv"
	"strings"
)

// specMarker is the literal the native parser searches for (see
// research/findings/magic-pop-biiv-server-v1188.md).
const specMarker = "[BIIV]"

// specMaxTokens is the native token cap (the parser compares against 5).
const specMaxTokens = 5

// specMaxPlus is the O clamp the row loader applies (it compares against 12).
const specMaxPlus = 12

// SpecToken is one parsed "<X:...>" token.
type SpecToken struct {
	// Letter is D, M, O or V (the native switch knows no other).
	Letter byte
	// Name is the M option's short name ("str"), Tag its packed magic
	// option tag (magicoption.txt column 8), Level the option degree the
	// native resolver picks the option row by.
	Name  string
	Tag   uint32
	Level uint8
	// Value is the token's integer: the plus level (O), the variance word
	// (V), the durability (D) or the option value (M).
	Value int64
}

// Spec is the merged property spec of one prize row; the zero value is
// the v1.150 "no override" spec.
type Spec struct {
	Tokens []SpecToken
}

/*
================
ParseSpec

One type-1 spec string. Unlike the native parser, which ignores a string
without the marker, a malformed string fails: admitting a prize row
whose override cannot be read would hand out the wrong item.
================
*/
func ParseSpec(text string) (Spec, error) {
	var spec Spec
	if !strings.HasPrefix(text, specMarker) {
		return spec, fmt.Errorf("gacha: spec %q lacks the %s marker", text, specMarker)
	}
	rest := text[len(specMarker):]
	for rest != "" {
		if rest[0] != '<' {
			return Spec{}, fmt.Errorf("gacha: spec %q has text outside a token", text)
		}
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			return Spec{}, fmt.Errorf("gacha: spec %q has an unterminated token", text)
		}
		token, err := parseSpecToken(rest[1:end])
		if err != nil {
			return Spec{}, fmt.Errorf("gacha: spec %q: %w", text, err)
		}
		if err := spec.add(token); err != nil {
			return Spec{}, fmt.Errorf("gacha: spec %q: %w", text, err)
		}
		rest = rest[end+1:]
	}
	return spec, nil
}

/*
================
parseSpecToken

"X:body": M is "name,level,value"; D, O and V hold one integer.
================
*/
func parseSpecToken(body string) (SpecToken, error) {
	if len(body) < 3 || body[1] != ':' {
		return SpecToken{}, fmt.Errorf("token %q is not X:value", body)
	}
	token := SpecToken{Letter: body[0]}
	value := body[2:]
	switch token.Letter {
	case 'D', 'O', 'V':
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return SpecToken{}, fmt.Errorf("token %q: %w", body, err)
		}
		token.Value = parsed
		if token.Letter == 'O' {
			token.Value = min(max(token.Value, 0), specMaxPlus)
		}
	case 'M':
		parts := strings.Split(value, ",")
		if len(parts) != 3 {
			return SpecToken{}, fmt.Errorf("token %q is not M:name,level,value", body)
		}
		tag, err := packOptionTag(parts[0])
		if err != nil {
			return SpecToken{}, fmt.Errorf("token %q: %w", body, err)
		}
		level, err := strconv.ParseUint(parts[1], 10, 8)
		if err != nil || level == 0 {
			return SpecToken{}, fmt.Errorf("token %q has invalid level %q", body, parts[1])
		}
		parsed, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || parsed <= 0 || parsed > 0xFFFFFFFF {
			return SpecToken{}, fmt.Errorf("token %q has invalid value %q", body, parts[2])
		}
		token.Name, token.Tag, token.Level, token.Value = parts[0], tag, uint8(level), parsed
	default:
		return SpecToken{}, fmt.Errorf("token %q has unknown letter %q", body, token.Letter)
	}
	return token, nil
}

/*
================
packOptionTag

The magic option tag packs its short name big-endian ("int" packs as the
decimal 6909556, the value magicoption.txt's tag column carries).
================
*/
func packOptionTag(name string) (uint32, error) {
	if len(name) == 0 || len(name) > 4 {
		return 0, fmt.Errorf("option name %q must be 1..4 letters", name)
	}
	var tag uint32
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < 'a' || c > 'z' {
			return 0, fmt.Errorf("option name %q must be lower-case letters", name)
		}
		tag = tag<<8 | uint32(c)
	}
	return tag, nil
}

/*
================
Spec.add

The native duplicate and count rules (see
research/findings/magic-pop-biiv-server-v1188.md).
================
*/
func (s *Spec) add(token SpecToken) error {
	if len(s.Tokens) >= specMaxTokens {
		return fmt.Errorf("more than %d tokens", specMaxTokens)
	}
	for _, existing := range s.Tokens {
		if existing.Letter != token.Letter {
			continue
		}
		if token.Letter != 'M' || existing.Tag == token.Tag {
			return fmt.Errorf("repeats token %c", token.Letter)
		}
	}
	s.Tokens = append(s.Tokens, token)
	return nil
}

/*
================
Spec.merge

The row's four spec columns feed one native param object (the loader loops
four pairs into one record), so the rules span the columns.
================
*/
func (s *Spec) merge(other Spec) error {
	for _, token := range other.Tokens {
		if err := s.add(token); err != nil {
			return err
		}
	}
	return nil
}

// token returns the first token with the letter.
func (s Spec) token(letter byte) (SpecToken, bool) {
	for _, token := range s.Tokens {
		if token.Letter == letter {
			return token, true
		}
	}
	return SpecToken{}, false
}

// Plus is the O override (override flag 4).
func (s Spec) Plus() (uint8, bool) {
	token, ok := s.token('O')
	return uint8(token.Value), ok
}

// Variance is the V override (override flag 1).
func (s Spec) Variance() (uint64, bool) {
	token, ok := s.token('V')
	return uint64(token.Value), ok
}

// Durability is the D override (override flag 8).
func (s Spec) Durability() (int64, bool) {
	token, ok := s.token('D')
	return token.Value, ok
}

// MagicOptions are the M overrides in spec order (override flag 2).
func (s Spec) MagicOptions() []SpecToken {
	var options []SpecToken
	for _, token := range s.Tokens {
		if token.Letter == 'M' {
			options = append(options, token)
		}
	}
	return options
}

// needsEquipment reports a token the row loader admits only on
// equipment (O, M and D; see research/findings/magic-pop-biiv-server-v1188.md).
func (s Spec) needsEquipment() bool {
	for _, token := range s.Tokens {
		if token.Letter != 'V' {
			return true
		}
	}
	return false
}

// key is a stable identity for comparing two rows' specs.
func (s Spec) key() string {
	var b strings.Builder
	for _, token := range s.Tokens {
		fmt.Fprintf(&b, "%c%d.%d.%d;", token.Letter, token.Tag, token.Level, token.Value)
	}
	return b.String()
}
