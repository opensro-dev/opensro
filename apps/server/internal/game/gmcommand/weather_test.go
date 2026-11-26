/*
===========================================================================

weather_test.go - the port's /WEATHER on the GM lane

Port-only, not native (action/weather.go): subcmd 0xFC with a u8 kind, a
u8 amount and a u16 minutes. The lane decodes it exactly, forwards it to
the action owner, and answers the owner's text on both results; a non-GM
sender is silent and reaches no owner.

===========================================================================
*/
package gmcommand

import (
	"bytes"
	"testing"
)

// weatherOwner records /WEATHER calls and answers with ok and text.
type weatherOwner struct {
	ok           bool
	text         string
	calls        int
	name         string
	kind, amount uint8
	minutes      uint16
}

func (o *weatherOwner) ToggleGMBodyStatus(string, string, uint8) bool { return false }

func (o *weatherOwner) SetWeatherGM(_, name string, kind, amount uint8, minutes uint16) (string, bool) {
	o.calls++
	o.name, o.kind, o.amount, o.minutes = name, kind, amount, minutes
	return o.text, o.ok
}

/*
================
TestWeatherDecodesExactlyFiveBytes
================
*/
func TestWeatherDecodesExactlyFiveBytes(t *testing.T) {
	request, err := DecodeGmCommand([]byte{SubWeather, 3, 200, 0x1E, 0x00})
	if err != nil || request.WeatherKind != 3 || request.WeatherAmount != 200 || request.WeatherMinutes != 30 {
		t.Fatalf("decode = %+v, %v; want snow 200 for 30", request, err)
	}
	for _, frame := range [][]byte{{SubWeather}, {SubWeather, 3, 200, 30}, {SubWeather, 3, 200, 30, 0, 0}} {
		if _, err := DecodeGmCommand(frame); err == nil {
			t.Fatalf("decode % X accepted a wrong length", frame)
		}
	}
}

/*
================
TestWeatherForwardsAndAnswersTheOwnersText
================
*/
func TestWeatherForwardsAndAnswersTheOwnersText(t *testing.T) {
	gm := gmChar("Gm", true, 0x1000)
	owner := &weatherOwner{ok: true, text: "snow 200 for 30 min on climate 2004"}
	out := HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, []byte{SubWeather, 3, 200, 30, 0}, owner)
	if owner.calls != 1 || owner.name != "Gm" || owner.kind != 3 || owner.amount != 200 || owner.minutes != 30 {
		t.Fatalf("owner saw %+v, want one call for Gm: snow 200 for 30", owner)
	}
	if want := EncodeModerationReply(AckResultOK, SubWeather, owner.text); !bytes.Equal(out.Ack, want) {
		t.Fatalf("ack % X, want % X", out.Ack, want)
	}
	owner.ok, owner.text = false, "the weather cycle is off (SRO_WEATHER)"
	out = HandleGmCommand(testDeps(gm), &stubPresence{}, testDivision, gm, []byte{SubWeather, 2, 0, 0, 0}, owner)
	if want := EncodeModerationReply(AckResultFail, SubWeather, owner.text); !bytes.Equal(out.Ack, want) || out.Refusal == "" {
		t.Fatalf("refused ack % X (%q), want % X with a reason", out.Ack, out.Refusal, want)
	}
}

/*
================
TestWeatherNeedsPrivilege
================
*/
func TestWeatherNeedsPrivilege(t *testing.T) {
	player := gmChar("Player", false, 0x1000)
	owner := &weatherOwner{ok: true}
	out := HandleGmCommand(testDeps(player), &stubPresence{}, testDivision, player, []byte{SubWeather, 3, 0, 0, 0}, owner)
	if out.Ack != nil || owner.calls != 0 {
		t.Fatalf("non-GM got ack % X and %d owner call(s), want silence and none", out.Ack, owner.calls)
	}
}
