/*
===========================================================================

christmas_decoration_test.go - the decoration-only Christmas mode

===========================================================================
*/
package progression

import (
	"testing"
	"time"
)

/*
================
TestChristmasDecorationRunsAlone

Mode decoration runs the decoration alone: neither the winter event nor
So-Ok's Christmas Gift runs beside it, and no other mode runs it. Off,
or outside the window, it does not run either.
================
*/
func TestChristmasDecorationRunsAlone(t *testing.T) {
	inside := time.Date(2026, 12, 28, 12, 0, 0, 0, time.UTC)
	t.Setenv(EnvChristmasEvent, "on")
	t.Setenv(EnvChristmasEventWindow, sookTestWindow)
	for _, mode := range []string{"decoration", "DECORATION", " decoration "} {
		t.Setenv(EnvChristmasEventMode, mode)
		if !ChristmasDecorationFromEnv().ActiveAt(inside) {
			t.Fatalf("mode %q: the decoration does not run", mode)
		}
		if ChristmasEventFromEnv().Enabled() || SoOkChristmasFromEnv().Enabled() {
			t.Fatalf("mode %q: another Christmas event runs beside the decoration", mode)
		}
	}
	if ChristmasDecorationFromEnv().ActiveAt(time.Date(2027, 1, 9, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("the decoration outlived its window")
	}
	for _, mode := range []string{"", "winter", "so_ok_2006", "santa"} {
		t.Setenv(EnvChristmasEventMode, mode)
		if ChristmasDecorationFromEnv().Enabled() {
			t.Fatalf("mode %q runs the decoration", mode)
		}
	}
	t.Setenv(EnvChristmasEvent, "off")
	t.Setenv(EnvChristmasEventMode, "decoration")
	if ChristmasDecorationFromEnv().ActiveAt(inside) {
		t.Fatal("off: the decoration runs")
	}
}
