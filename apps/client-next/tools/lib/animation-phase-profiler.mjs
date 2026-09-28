/*
===========================================================================

animation-phase-profiler.mjs - bounded sampling of pose materialization

Owns capture counters and sampled phase timers. Pose construction accepts
the observer explicitly; production pose math is shared with the capture.

===========================================================================
*/
// Diagnostic-only sampling. Never enabled in acceptance or shipping builds.
// Select evaluations pseudo-randomly so actor order cannot alias a fixed stride.
/*
================
createAnimationPhaseProfiler
================
*/
export function createAnimationPhaseProfiler( now = () => performance.now() ) {
	let seed = 240, eligible = 0, sampled = 0, reads = 0, active = false;
	const sums = {}, counts = {}, starts = {}, models = new WeakMap(), rows = new Map();
	let current = null;
	const describe = model => {
		let row = models.get( model );
		if ( !row ) {
			row = {
				id: model.primitives?.[0]?.name ?? "unknown",
				nodes: model.nodes.length,
				primitives: model.primitives.length
			};
			models.set( model, row );
		}
		return row;
	};
	const record = ( model, reason, count = 1 ) => {
		if ( !active || !model ) return null;
		const meta = describe( model ), key = meta.id + "|" + reason;
		if ( !rows.has( key ) && rows.size >= 512 ) return null;
		let row = rows.get( key );
		if ( !row ) {
			row = { ...meta, reason, calls: 0, sampled: 0, ms: 0 };
			rows.set( key, row );
		}
		row.calls += count;
		return row;
	};
	const timer = {
		phases: false, /*
================
start
================
		*/
		start( name ) {
			starts[name] = now();
			reads++;
		}, /*
================
end
================
		*/
		end( name ) {
			const elapsed = now() - starts[name];
			reads++;
			sums[name] = (sums[name] ?? 0) + elapsed;
			counts[name] = (counts[name] ?? 0) + 1;
			if ( name === "materialization" && current ) {
				current.ms += elapsed;
				current.sampled++;
			}
		}
	};
	return {
		/*
================
tag
================
		*/
		tag( model, id, plan ) {
			const meta = describe( model );
			meta.id = id;
			meta.sharedPalette = plan.sharedPalette;
		},
		admission: record,
		/*
================
start
================
		*/
		start() {
			rows.clear();
			current = null;
			active = true;
			eligible = sampled = reads = 0;
			seed = 240;
			for ( const name of Object.keys( sums ) ) delete sums[name];
			for ( const name of Object.keys( counts ) ) delete counts[name];
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
begin
================
		*/
		begin( model, reason = "evaluate", resolved ) {
			if ( !active ) return null;
			current = model ?
				record(
					model,
					reason + ":" + (resolved?.length > 1 ? "layered" : resolved?.[0]?.clip?.name ?? "rest")
				) :
				null;
			eligible++;
			seed ^= seed << 13;
			seed ^= seed >>> 17;
			seed ^= seed << 5;
			if ( (seed >>> 0) % 64 ) return null;
			sampled++;
			timer.phases = !!(seed & 256);
			return timer;
		},
		/*
================
stats
================
		*/
		stats() {
			return {
				eligible,
				sampled,
				clockReads: reads,
				materializations: [ ...rows.values() ],
				sums: { ...sums },
				counts: { ...counts },
				qualification:
					"Separate randomized samples measure total CPU materialization or inner phases, never both on the same call. Includes deferred fallback, sockets and portraits. Sampling includes quaternion time; do not add them. Timer overhead and interruptions are included. Not an acceptance or exact per-frame estimate."
			};
		}
	};
}

/*
================
instrumentAnimationPhases
================
*/
export function instrumentAnimationPhases( source ) {
	// Startup passes the capture owner through RuntimeDiagnostics.
	return source;
}

/*
================
instrumentAnimationAdmission
================
*/
export function instrumentAnimationAdmission( source, file ) {
	const replace = ( marker, value ) => {
		if ( source.split( marker ).length !== 2 ) throw Error( "Animation admission boundary changed: " + marker );
		source = source.replace( marker, value );
	};
	if ( file === "src/engine/runtime/renderer/device/geometry.ts" ) {
		replace(
			"current();const palette=sharedPalettes.get(source);if(!palette)return false;",
			"current();const palette=sharedPalettes.get(source);if(!palette){globalThis.__worldProbeAnimationPhases?.admission(model,'gpu:palette-not-uploaded',samples.length);return false;}"
		);
	}
	if ( file === "src/engine/runtime/renderer/device/animation.ts" ) {
		replace(
			"if(!pipeline||unsupported.has(model)||!samples.some(s=>s!==null)||!primitive.joints.length)return false;",
			"if(!pipeline||unsupported.has(model)||!samples.some(s=>s!==null)||!primitive.joints.length){globalThis.__worldProbeAnimationPhases?.admission(model,!pipeline?'gpu:cold':model.nodes.length>128?'gpu:rig-limit':'gpu:previous-refusal',samples.length);return false;}"
		);
		replace(
			"if(!plan||staticBytes+plan.data.byteLength>STATIC_BYTES){unsupported.add(model);return false;}",
			"if(!plan||staticBytes+plan.data.byteLength>STATIC_BYTES){globalThis.__worldProbeAnimationPhases?.admission(model,model.nodes.length>128?'gpu:rig-limit':!plan?'gpu:plan-refused':'gpu:static-budget',samples.length);unsupported.add(model);return false;}"
		);
		replace(
			"pending.add(row);return true;",
			"pending.add(row);globalThis.__worldProbeAnimationPhases?.admission(model,'gpu:accepted',samples.filter(s=>s!==null).length);return true;"
		);
	}
	return source;
}
