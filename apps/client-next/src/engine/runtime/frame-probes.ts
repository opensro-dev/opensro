/*
===========================================================================

frame-probes.ts - the profiler's frame observer, as the frame owner sees it

A benchmark run (tools/perf/bench) installs its frame probe on globalThis
before startup; this finds it and hands runtime.ts one probe to pass down
the frame. Production builds compile the lookup away.

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
	movement?( sample: {
		atMs: number;
		workerAtMs: number;
		workerDebtMs: number;
		revision: number;
		transition?: import("@/engine/contracts/gameplay").MovementTransition;
		logical: import("@/engine/contracts/gameplay").Pose;
		displayed: import("@/engine/contracts/gameplay").Pose | null;
		pending: number;
		acknowledged: number;
	} ): void;
}

/*
================
frameProbe

The benchmark's frame probe, installed on globalThis by benchmark runs of
the development client only; otherwise undefined. The frame calls it
explicitly instead of letting a profiler patch this source.
================
*/
export function frameProbe(): FrameProbe | undefined {
	if ( !import.meta.env.DEV ) return undefined;
	return (globalThis as { __worldProbeFrameProfiler?: FrameProbe; }).__worldProbeFrameProfiler;
}
