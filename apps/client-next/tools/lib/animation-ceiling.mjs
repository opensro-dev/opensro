/*
===========================================================================

animation-ceiling.mjs - bounded diagnostic pose and world replay

Capture windows may replay warmed state to measure an upper bound on savings.
These interventions never enter acceptance or shipping builds.

===========================================================================
*/
// Diagnostic intervention, never shipping optimization. Only the owned capture
// window freezes warmed skeletal poses. Actor/world transforms remain live.
/*
================
createAnimationCeiling
================
*/
export function createAnimationCeiling( freeze = false, alternate = false, combined = false ) {
	let mode = 0,
		worldCalls = 0,
		worldSkipped = 0,
		worldCold = 0,
		replay = freeze,
		frames = 0,
		active = false,
		eligible = 0,
		skipped = 0,
		cold = 0,
		builds = 0,
		copies = 0,
		joints = 0;
	return {
		/*
================
start
================
		*/
		start() {
			active = true;
			frames = 0;
			mode =
				worldCalls =
				worldSkipped =
				worldCold =
					0;
			replay = freeze;
			eligible =
				skipped =
				cold =
				builds =
				copies =
				joints =
					0;
		},
		/*
================
pause
================
		*/
		pause() {
			active = false;
		},
		/*
================
frame
================
		*/
		frame() {
			if ( active && combined ) {
				mode = [ 0, 1, 3, 2, 2, 3, 1, 0 ][Math.floor( frames++ / 16 ) % 8];
				replay = Boolean( mode & 1 );
			} else if ( active && alternate ) replay = Math.floor( frames++ / 16 ) % 2 === 1;
			return active && replay ? 1 : 0;
		},
		/*
================
worldMode
================
		*/
		worldMode() {
			return active && combined && (mode & 2) ? 1 : 0;
		},
		/*
================
worldReplay
================
		*/
		worldReplay( ready ) {
			if ( !active || !combined ) return false;
			worldCalls++;
			if ( !ready ) {
				worldCold++;
				return false;
			}
			if ( mode & 2 ) {
				worldSkipped++;
				return true;
			}
			return false;
		},
		/*
================
skip
================
		*/
		skip( ready, allowed ) {
			if ( !active || !allowed ) return false;
			eligible++;
			if ( !ready ) {
				cold++;
				return false;
			}
			if ( replay ) {
				skipped++;
				return true;
			}
			return false;
		},
		/*
================
palette
================
		*/
		palette( built, count, allowed ) {
			if ( !active || !allowed ) return;
			copies++;
			if ( built ) {
				builds++;
				joints += count;
			}
		},
		/*
================
stats
================
		*/
		stats() {
			return {
				freeze,
				alternate,
				combined,
				worldCalls,
				worldSkipped,
				worldCold,
				eligible,
				skipped,
				cold,
				paletteBuilds: builds,
				paletteCopies: copies,
				paletteJointsBuilt: joints,
				qualification:
					"Diagnostic frozen skeletal poses and optional stale world selection; visual fidelity intentionally fails. Current camera transforms and draws remain live. Palette copying/upload and renderer bookkeeping remain. Particle/ribbon models excluded from pose replay. Includes eligible portraits."
			};
		}
	};
}
/*
================
instrumentAnimationCeiling
================
*/
export function instrumentAnimationCeiling( source ) {
	// Startup passes the capture owner through RuntimeDiagnostics.
	return source;
}
