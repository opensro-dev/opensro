/*
===========================================================================

frame-profiler.mjs - the world probe's per-frame stage recorder

createFrameProfiler records per-frame stage timings and draw counts into a
bounded numeric buffer the probe exports after a capture window. Every owner
calls it through explicit hooks: runtime.ts installs it as the frame probe
(frameProbe) and passes it to the UI, renderer, world, frame and character
owners. No client source is patched.

===========================================================================
*/

// Browser-owned bounded numeric recorder. Export/serialization happens after the window.
/*
================
createFrameProfiler
================
*/
export function createFrameProfiler( now = () => performance.now(), capacity = 32768 ) {
	const columns = [
		"frameId",
		"startMs",
		"cpuMs",
		"input-state-frontend",
		"character-presentation",
		"world-stream",
		"ui",
		"render-preparation-submit",
		"hover",
		"audio",
		"renderer-setup",
		"world-prepare",
		"character-prepare",
		"labels-portraits",
		"submit",
		"world-camera",
		"world-environment",
		"world-selection",
		"world-finalize",
		"character-plan",
		"character-poses",
		"character-upload",
		"terrain-candidates",
		"terrain-indices",
		"terrain-seams",
		"terrain-index-upload",
		"terrain-position-upload",
		"ui-assembly",
		"ui-finalize",
		"ui-compare",
		"ui-publish",
		"animation-replay",
		"world-replay"
	];
	columns.push( "actor-motion", "actor-sounds", "actor-record" );
	columns.push(
		"presentation-selection",
		"presentation-events",
		"presentation-state",
		"presentation-actors",
		"presentation-finalize",
		"world-instance-setup",
		"world-instance-loop",
		"world-instance-upload",
		"world-detail-sampled"
	);
	columns.push( "pose-created", "pose-retired", "pose-evaluations" );
	const indices = new Map( columns.map( ( name, i ) => [ name, i ] ) ),
		stride = columns.length,
		data = new Float64Array( capacity * stride );
	const detailStarts = new Map();
	const drawSamples = [];
	let nextDrawSample = 0;
	let active = false,
		count = 0,
		dropped = 0,
		at = -1,
		stageAt = 0,
		renderAt = 0,
		worldAt = 0,
		characterAt = 0,
		renderName = "",
		characterActive = false;
	return {
		/*
		================
		start
		================
		*/
		start() {
			count = dropped = 0;
			at = -1;
			active = true;
			drawSamples.length = 0;
			nextDrawSample = 0;
		},
		/*
		================
		begin
		================
		*/
		begin( frameId ) {
			if ( !active ) return;
			if ( count === capacity ) {
				dropped++;
				at = -1;
				return;
			}
			at = count * stride;
			data.fill( 0, at, at + stride );
			data[at] = frameId;
			data[at + 1] = stageAt = now();
			data[at + indices.get( "animation-replay" )] = globalThis.__worldProbeAnimationCeiling?.frame() ?? 0;
			data[at + indices.get( "world-replay" )] = globalThis.__worldProbeAnimationCeiling?.worldMode?.() ?? 0;
		},
		/*
		================
		mark
		================
		*/
		mark( name ) {
			if ( !active || at < 0 ) return;
			const index = indices.get( name );
			if ( index === undefined ) throw Error( "Unknown frame stage " + name );
			const end = now();
			data[at + index] += end - stageAt;
			stageAt = end;
		},
		/*
		================
		renderBegin
		================
		*/
		renderBegin() {
			if ( active && at >= 0 ) renderAt = now();
		},
		/*
		================
		renderMark
		================
		*/
		renderMark( name ) {
			if ( !active || at < 0 ) return;
			const index = indices.get( name );
			if ( index === undefined ) throw Error( "Unknown renderer stage " + name );
			const end = now();
			data[at + index] += end - renderAt;
			renderAt = end;
			renderName = name;
		},
		/*
		================
		worldBegin
		================
		*/
		worldBegin() {
			if ( active && at >= 0 ) worldAt = now();
		},
		/*
		================
		worldMark
		================
		*/
		worldMark( name ) {
			if ( !active || at < 0 ) return;
			const end = now();
			data[at + indices.get( name )] += end - worldAt;
			worldAt = end;
		},
		/*
		================
		characterBegin
		================
		*/
		characterBegin() {
			characterActive = active && at >= 0 && renderName === "world-prepare";
			if ( characterActive ) characterAt = now();
		},
		/*
		================
		characterMark
		================
		*/
		characterMark( name ) {
			if ( !characterActive || !active || at < 0 ) return;
			const end = now();
			data[at + indices.get( name )] += end - characterAt;
			characterAt = end;
		},
		/*
		================
		characterCount
		================
		*/
		characterCount( name, value = 1 ) {
			if ( characterActive && active && at >= 0 ) data[at + indices.get( name )] += value;
		},
		/*
		================
		sampleDetails
		================
		*/
		sampleDetails() {
			if ( !active || at < 0 || count % 32 !== 0 ) return false;
			data[at + indices.get( "world-detail-sampled" )] = 1;
			return true;
		},
		/*
		================
		detailBegin
		================
		*/
		detailBegin( name ) {
			if ( active && at >= 0 ) detailStarts.set( name, now() );
		},
		/*
		================
		detailEnd
		================
		*/
		detailEnd( name ) {
			if ( !active || at < 0 ) return;
			const start = detailStarts.get( name ), index = indices.get( name );
			if ( start === undefined || index === undefined ) throw Error( "Invalid detail span " + name );
			data[at + index] += now() - start;
			detailStarts.delete( name );
		},
		/*
		================
		end
		================
		*/
		end() {
			if ( !active || at < 0 ) return;
			data[at + 2] = now() - data[at + 1];
			count++;
			at = -1;
		},
		// Count submitted primitives, not visible pixels or occluded fragments. The
		// sample cadence bounds diagnostic work; none of this ships in normal builds.
		/*
		================
		frameDraw
		================
		*/
		frameDraw( frameId, image, geometry, world, ui, preview, flares, thunder, portrait, doll, partyPortraits ) {
			if ( !active ) return;
			const sampledAt = now();
			if ( sampledAt < nextDrawSample || drawSamples.length >= 1024 ) return;
			nextDrawSample = sampledAt + 500;
			/*
			================
			tally
			================
			*/
			const tally = () => ({ draws: 0, instances: 0, triangles: 0, zeroDraws: 0, duplicateReferences: 0 });
			const main = tally(), overlay = tally(), portraits = tally(), seen = new Set();
			/*
			================
			add
			================
			*/
			function add( row, draw ) {
				row.draws++;
				row.instances += draw.instanceCount;
				row.triangles += draw.indexCount / 3 * draw.instanceCount;
				if ( !draw.instanceCount || !draw.indexCount ) row.zeroDraws++;
				if ( seen.has( draw ) ) row.duplicateReferences++;
				seen.add( draw );
			}
			if ( geometry ) add( main, geometry );
			for ( const draw of world ) add( main, draw );
			seen.clear();
			for ( const draw of preview ) add( overlay, draw );
			let portraitPasses = 0;
			for ( const product of [ portrait, ...partyPortraits, doll ] ) {
				if ( product ) {
					portraitPasses++;
					seen.clear();
					for ( const draw of product.draws ) add( portraits, draw );
				}
			}
			let uiQuads = 0, uiDraws = 0;
			for ( const draw of ui ) {
				if ( draw.layer === undefined || draw.layer === "background" || draw.layer === "world" ) {
					uiQuads += draw.count;
					uiDraws++;
				}
			}
			const flareVertices = flares?.entries.reduce( ( sum, draw ) => sum + draw.count, 0 ) ?? 0;
			drawSamples.push( {
				frameId,
				at: sampledAt,
				main,
				preview: overlay,
				portraits,
				portraitPasses,
				uiQuads,
				uiDraws,
				flareVertices,
				imageTriangles: image ? 2 : 0,
				thunderTriangles: thunder ? 2 : 0,
				renderPasses: 1 + portraitPasses + Number( !!thunder ) + Number( !!flares ) +
					Number( !!flares || !!thunder ) + Number( preview.length > 0 ),
				computePasses: Number( !!flares )
			} );
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
		stop
		================
		*/
		stop() {
			active = false;
			return {
				columns,
				rows: Array.from(
					{ length: count },
					( _, i ) => Array.from( data.subarray( i * stride, (i + 1) * stride ) )
				),
				dropped,
				drawSamples: [ ...drawSamples ]
			};
		}
	};
}
