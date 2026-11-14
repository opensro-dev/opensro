/*
===========================================================================

hidden-render.ts - which frames still render while no display asks for one

A hidden tab, or a visible window that gets no RAF (starved), runs
maintenance frames from worker deliveries. Those frames skip GPU
preparation, because nothing can be shown. Scene admission happens only
inside the renderer frame, though, so a load that is waiting on admission
never finishes without one.

The native client keeps updating while inactive and only stops drawing:
Application_PumpIdleFrame (A0F430) runs OnUpdate every idle frame,
CGFXMainFrame_RenderFrame (A11650) sleeps instead of drawing while the
window is inactive, and loading finishes in OnUpdate. So a background tab
finishes its loads too: the world entry (0x3012 game-ready waits on it) and
the frontend's loading phases. The frontend's stall watch runs on the step
clock, which hidden maintenance frames still advance; without a renderer
frame it declared a backgrounded dock or creation load "Loading stalled"
after 60 s.

===========================================================================
*/
import type { FrontendPhase } from "@/engine/contracts/frontend";

// The frontend phases whose completion gate waits on a stage or preview
// scene being admitted by the renderer (frontend.ts stepLoadingWatch).
const FRONTEND_LOADING_PHASES: readonly FrontendPhase[] = [
	"loading-title",
	"loading-dock",
	"loading-create",
	"loading-race"
] as const;

/*
================
hiddenFrameRenders

True when a frame that no display asked for must still run the renderer,
because a load is waiting on scene admission.
================
*/
export function hiddenFrameRenders(
	frontendPhase: FrontendPhase | undefined,
	sessionPhase: string | undefined,
	readySent: boolean
): boolean {
	if ( !readySent && sessionPhase === "world" ) return true;
	return frontendPhase !== undefined && FRONTEND_LOADING_PHASES.includes( frontendPhase );
}
