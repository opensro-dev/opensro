/*
===========================================================================

frame-probes.ts - the profiler's observers, as the frame owner sees them

Probe runs (tools/profile-world.mjs and the release profile build) install
their frame profiler, animation ceiling and pick census on globalThis
before startup. These functions find them and hand runtime.ts one probe to
pass down the frame; production builds compile them to undefined.

===========================================================================
*/
import type { RenderFrameProbe } from "@/engine/contracts/runtime";

/*
================
FrameProbe

Development frame timing, including detail spans reported by UI owners.
================
*/
export interface FrameProbe extends Omit<RenderFrameProbe, "detailBegin" | "detailEnd"> {
	// The UI owner's detail spans (ui.ts UiFrameProbe), required here.
	detailBegin( stage: string ): void;
	detailEnd( stage: string ): void;
	sampleDetails(): boolean;
	begin( frameId: number ): void;
	mark( stage: string ): void;
	end(): void;
}

/*
================
PROFILING_BUILD

Development, or a profile build of the release (tools/lib/release-profile.mjs
defines __SRO_PROFILE_BUILD__). A production build defines neither. The typeof
guard keeps the constant safe where no bundler defines it (tests, tools).
================
*/
declare const __SRO_PROFILE_BUILD__: boolean | undefined;
const PROFILING_BUILD = import.meta.env.DEV ||
	(typeof __SRO_PROFILE_BUILD__ !== "undefined" && __SRO_PROFILE_BUILD__ === true);

/*
================
idleFrameProbe

Stands in for the frame profiler when only the ceiling or pick census is
installed, so the frame owners still have one probe to call.
================
*/
const idleFrameProbe: FrameProbe = Object.freeze( {
	detailBegin() {},
	detailEnd() {},
	renderBegin() {},
	renderMark() {},
	characterBegin() {},
	characterMark() {},
	characterCount() {},
	sampleDetails: () => false,
	begin() {},
	mark() {},
	end() {}
} );

/*
================
frameProbe

The world probe's frame profiler (tools/lib/frame-profiler.mjs), installed
on globalThis by probe runs only; production leaves it undefined. The frame
calls it explicitly instead of letting the profiler patch this source. The
animation ceiling's world replay and the pick census join it when installed.
================
*/
export function frameProbe(): FrameProbe | undefined {
	if ( !PROFILING_BUILD ) return undefined;
	const installed = globalThis as {
		__worldProbeFrameProfiler?: FrameProbe;
		__worldProbeAnimationCeiling?: { worldReplay?( hasView: boolean ): boolean; };
		__worldProbePickCensus?: ( row: import("@/engine/contracts/runtime").WorldPickSample ) => void;
	};
	const frame = installed.__worldProbeFrameProfiler,
		ceiling = installed.__worldProbeAnimationCeiling,
		pickCensus = installed.__worldProbePickCensus;
	if ( !ceiling?.worldReplay && !pickCensus ) return frame;
	return {
		...(frame ?? idleFrameProbe),
		// The capture's methods close over their own state (no this binding).
		...(ceiling?.worldReplay ? { worldReplay: ceiling.worldReplay } : {}),
		...(pickCensus ? { pickCensus } : {})
	};
}
/*
================
animationProbe

Capture owners are installed before startup. Pass their observers through
renderer construction so profiling never replaces the pose implementation.
================
*/
export function animationProbe():
	| import("@/engine/foundation/animation/animation-pose").AnimationPoseProbe
	| undefined {
	if ( !PROFILING_BUILD ) return undefined;
	const captures = globalThis as {
		__worldProbeAnimationCeiling?:
			import("@/engine/foundation/animation/animation-pose").AnimationPoseProbe["ceiling"];
		__worldProbeAnimationPhases?:
			import("@/engine/foundation/animation/animation-pose").AnimationPoseProbe["phases"];
	};
	if ( !captures.__worldProbeAnimationCeiling && !captures.__worldProbeAnimationPhases ) return undefined;
	return { ceiling: captures.__worldProbeAnimationCeiling, phases: captures.__worldProbeAnimationPhases };
}
