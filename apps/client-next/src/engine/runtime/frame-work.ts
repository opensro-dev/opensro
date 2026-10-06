/*
===========================================================================

frame-work.ts - CPU overload hysteresis and the shared optional-work budget

The runtime supplies measured CPU time, excluding its readback wait. This
owner never changes saved graphics preferences or admits gameplay events.

===========================================================================
*/
const CPU_BUDGET_MS = 1000 / 60;
const HEADROOM_MS = 10;
const REDUCE_AFTER_MS = 1000;
const RESTORE_AFTER_MS = 5000;
const MAX_OBSERVATION_GAP_MS = 250;
const OPTIONAL_WORK_MS = 2;
const MAX_LEVEL = 2;
const RECENT_FRAME_LIMIT = 32;
const LONG_FRAME_MS = 50;

/*
================
createFrameWork

Only displayed, visible intervals contribute to sustained overload. One
slow callback or a hidden-tab gap cannot change the player's detail level.
================
*/
export function createFrameWork() {
	let level = 0, over = 0, headroom = 0, last: number | undefined;
	let interval = 0, remaining = OPTIONAL_WORK_MS;
	let longFrames = 0;
	const recent: { atMs: number; cpuMs: number; level: number; }[] = [];
	return {
		/*
		================
		begin
		================
		*/
		begin( now: number, visible: boolean ) {
			interval = visible && last !== undefined && now >= last && now - last <= MAX_OBSERVATION_GAP_MS ?
				now - last :
				0;
			if ( !interval ) over = headroom = 0;
			last = visible ? now : undefined;
			remaining = OPTIONAL_WORK_MS;
		},
		/*
		================
		recordCpu
		================
		*/
		recordCpu( cpuMs: number ) {
			if ( !interval || !Number.isFinite( cpuMs ) || cpuMs < 0 ) return;
			if ( cpuMs > LONG_FRAME_MS ) {
				longFrames++;
				recent.push( { atMs: last!, cpuMs, level } );
				if ( recent.length > RECENT_FRAME_LIMIT ) recent.shift();
			}
			over = cpuMs > CPU_BUDGET_MS ? over + interval : 0;
			headroom = cpuMs < HEADROOM_MS ? headroom + interval : 0;
			if ( over >= REDUCE_AFTER_MS ) {
				level = Math.min( MAX_LEVEL, level + 1 );
				over = headroom = 0;
			} else if ( headroom >= RESTORE_AFTER_MS ) {
				level = Math.max( 0, level - 1 );
				over = headroom = 0;
			}
		},
		/*
		================
		level
		================
		*/
		level() {
			return level;
		},
		/*
		================
		stats
		================
		*/
		stats() {
			return { level, longFrames, recent: recent.map( row => ({ ...row }) ) };
		},
		/*
		================
		remaining
		================
		*/
		remaining() {
			return remaining;
		},
		/*
		================
		spend

		Call after each resumable optional unit. No subsequent unit starts
		once the aggregate budget is exhausted; resident poses remain usable.
		================
		*/
		spend( ms: number ) {
			remaining = Math.max( 0, remaining - Math.max( 0, ms ) );
		}
	};
}
