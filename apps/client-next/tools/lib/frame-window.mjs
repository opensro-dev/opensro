// Serializable browser-side recorder. Input and observation share this window's
// clock, so host/Playwright scheduling cannot lengthen the movement recipe.
export function captureFrameWindow( { duration, name, movement, camera, active, deferExport = false } ) {
	return new Promise( resolve => {
		globalThis.__worldProbeFrameProfiler?.start();
		globalThis.__worldProbeAnimationCeiling?.start();
		globalThis.__worldProbeAnimationPhases?.start();
		globalThis.__worldProbeUiProducts?.start();
		performance.mark( `world-profile:${name}:start` );
		const times = [],
			telemetry = [],
			motion = [],
			commands = [],
			start = performance.now(),
			startFrameId = globalThis.__worldProbeRenderedFrame;
		let previous = globalThis.__worldProbeFrameTelemetry, turn = -1, nextMotion = 0, activeTurn = -1;
		function tick( t ) {
			const elapsed = t - start;
			times.push( t );
			// Render-work isolation, not a physical mouse/input-latency benchmark.
			// Sample an elapsed-time path once per browser frame, without CDP round trips.
			if ( camera && elapsed < duration ) {
				const phase = elapsed / duration * Math.PI * 2;
				document.querySelector( "canvas" ).dispatchEvent(
					new PointerEvent( "pointermove", {
						bubbles: true,
						pointerId: 1,
						pointerType: "mouse",
						buttons: 2,
						clientX: camera.x + 120 * Math.sin( phase ),
						clientY: camera.y + 25 * Math.sin( phase * 2 )
					} )
				);
			}
			if ( movement && elapsed < duration ) {
				const nextTurn = Math.floor( elapsed / 1500 );
				if ( nextTurn !== turn ) {
					turn = nextTurn;
					const destination = turn % 2 ? movement.original : movement.destination;
					globalThis.__worldProbeRoot.session( { kind: "gameplay", command: { kind: "move", destination } } );
					commands.push( { at: t, destination } );
				}
			}
			// Active play: two-second turns alternate a walk with an imbue press and an
			// attack on the nearest living monster; the camera drag runs throughout.
			if ( active && elapsed < duration ) {
				const nextTurn = Math.floor( elapsed / 2000 );
				if ( nextTurn !== activeTurn ) {
					activeTurn = nextTurn;
					const root = globalThis.__worldProbeRoot, game = root.gameplay();
					if ( nextTurn % 2 === 0 ) {
						const destination = nextTurn % 4 ? active.original : active.destination;
						root.session( { kind: "gameplay", command: { kind: "move", destination } } );
						commands.push( { at: t, move: destination } );
					} else {
						const pose = game.pose,
							world = e => [ ((e.regionId & 255) * 1920) + e.x, ((e.regionId >>> 8) * 1920) + e.z ];
						const here = world( pose ),
							monsters = root.entities().filter( e =>
								e.kind === "monster" && !(e.appearanceState?.[0] === 2)
							).map( e => {
								const p = world( e );
								return { gid: e.gid, d: Math.hypot( p[0] - here[0], p[1] - here[1] ) };
							} ).sort( ( a, b ) => a.d - b.d );
						if ( active.imbue && nextTurn === 1 ) {
							root.session( { kind: "gameplay", command: { kind: "skill", skillId: active.imbue } } );
							commands.push( { at: t, skill: active.imbue } );
						}
						if ( monsters[0] ) {
							root.session( { kind: "gameplay", command: { kind: "attack", gid: monsters[0].gid } } );
							commands.push( { at: t, attack: monsters[0].gid, distance: monsters[0].d } );
						}
					}
				}
			}
			if ( movement && (elapsed >= nextMotion || elapsed >= duration) ) {
				const game = globalThis.__worldProbeRoot.gameplay();
				motion.push( { at: t, pose: game.pose, moving: game.moving } );
				nextMotion = elapsed + 100;
			}
			const current = globalThis.__worldProbeFrameTelemetry;
			if ( current && current !== previous ) {
				telemetry.push( { at: t, sample: current } );
				previous = current;
			}
			if ( elapsed < duration ) requestAnimationFrame( tick );
			else {
				performance.mark( `world-profile:${name}:end` );
				globalThis.__worldProbeFrameProfiler?.pause();
				globalThis.__worldProbeAnimationCeiling?.pause();
				globalThis.__worldProbeAnimationPhases?.pause();
				globalThis.__worldProbeUiProducts?.pause();
				const result = {
					times,
					telemetry,
					motion,
					commands,
					start,
					end: t,
					startFrameId,
					endFrameId: globalThis.__worldProbeRenderedFrame
				};
				if ( deferExport ) {
					globalThis.__worldProbeCompletedWindow = result;
					resolve( null );
				} else resolve( result );
			}
		}
		requestAnimationFrame( tick );
	} );
}
