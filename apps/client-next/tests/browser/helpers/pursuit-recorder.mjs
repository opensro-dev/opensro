import assert from "node:assert/strict";

// Read-only observations at the admitted packet and motion owners. Consumed
// B738/B245 packets never reach the unhandled-native event stream. Keep both
// simulation time and wall-clock correlation: simulation time starts at zero
// and is advanced by fixed steps, not by worker performance.now.
// These response insertions add no commands, state assignments or fake packets.
export async function installPursuitRecorder( page, extraOpcodes = [] ) {
	const instrumentation = { core: false, motion: false };
	await page.route( "**/renderer/characters/characters.ts*", async route => {
		const response = await route.fetch(), body = await response.text();
		const anchors = /(labelAnchors\(origin,\s*view,\s*width,\s*height,\s*gids\)\s*\{[\s\S]*?)(return result;)/;
		assert.match( body, anchors, "visible model anchor owner changed" );
		await route.fulfill( {
			response,
			body: body.replace(
				anchors,
				`$1globalThis.__pursuitVisible={at:performance.now(),anchors:Object.fromEntries(result)}; $2`
			)
		} );
	} );
	await page.route( "**/world/core.ts*", async route => {
		const response = await route.fetch(), body = await response.text();
		const receive = /function receive\(frame,\s*now\)\s*\{/;
		const command = /command\(command,\s*now\)\s*\{/;
		const step = /step\(now,\s*advance\s*=\s*true\)\s*\{/;
		assert.match( body, receive, "packet owner changed; update recorder deliberately" );
		assert.match( body, command, "command owner changed; update recorder deliberately" );
		assert.match( body, step, "simulation step owner changed" );
		const observed = body.replace(
			receive,
			`$&
      if ([0xb738,0xb245,0xb505,0xb2f5,0x3122,...${JSON.stringify( extraOpcodes )}].includes(frame.opcode))
        globalThis.postMessage({kind:'pursuit-observation',event:{kind:'wire',at:now,wallAt:performance.now(),origin:performance.timeOrigin,opcode:frame.opcode,payload:[...frame.payload],targetBefore:entities.read(globalThis.__pursuitObservedTarget),playerBefore:entities.read(gameplay.localIdentity())}});
    `
		).replace(
			command,
			`$&
      if (command.kind==='attack') globalThis.__pursuitObservedTarget=command.gid;
    `
		).replace(
			step,
			`$&
      if (now%96===0) globalThis.postMessage({kind:'pursuit-observation',event:{kind:'clock',at:now,wallAt:performance.now(),origin:performance.timeOrigin}});
    `
		);
		instrumentation.core = true;
		await route.fulfill( { response, body: observed } );
	} );
	await page.route( "**/entities/motion/motion.ts*", async route => {
		const response = await route.fetch(), body = await response.text();
		assert.ok(
			body.includes( "setMotionSegmentObserver" ),
			"motion segment seam missing; update recorder deliberately"
		);
		instrumentation.motion = true;
		const registration =
			"\nsetMotionSegmentObserver((gid, segment) => globalThis.postMessage({kind:'pursuit-observation',event:{kind:'segment',at:segment.start,origin:performance.timeOrigin,gid,segment}}));\n";
		const mapComment = body.lastIndexOf( "//# sourceMappingURL=" );
		await route.fulfill( {
			response,
			body: mapComment >= 0 ?
				body.slice( 0, mapComment ) + registration + body.slice( mapComment ) :
				body + registration
		} );
	} );
	await page.route( "**/gameplay/movement/movement.ts*", async route => {
		const response = await route.fetch(), body = await response.text();
		const seed = /seed\(value\)\s*\{/;
		assert.match( body, seed, "local movement owner changed" );
		await route.fulfill( {
			response,
			body: body.replace(
				seed,
				`$& globalThis.postMessage({kind:'pursuit-observation',event:{kind:'local-speed',speed,at:performance.now(),origin:performance.timeOrigin}});`
			)
		} );
	} );
	await page.addInitScript( () => {
		const Original = Worker;
		globalThis.__pursuit = {
			entities: {},
			segments: {},
			events: [],
			samples: [],
			mainOrigin: performance.timeOrigin
		};
		globalThis.Worker = class extends Original {
			constructor( ...args ) {
				super( ...args );
				this.addEventListener( "message", ( { data } ) => {
					const r = globalThis.__pursuit;
					if ( data.kind === "failure" ) {
						r.events.push( { kind: "worker-failure", message: data.message, at: performance.now() } );
					}
					if ( data.kind === "pursuit-observation" ) {
						const event = { ...data.event, mainReceivedAt: performance.now() };
						r.events.push( event );
						if ( event.kind === "segment" ) r.segments[event.gid] = event;
						if ( event.kind === "local-speed" ) r.localSpeed = event.speed;
						if ( event.kind === "clock" ) r.clock = event;
						if ( r.events.length > 4000 ) r.events.splice( 0, 1000 );
					}
					if ( data.kind !== "world" || !data.batch ) return;
					for ( const e of data.batch.events ) {
						if ( e.kind === "state" || e.kind === "spawn" ) r.entities[e.entity.gid] = e.entity;
						if ( e.kind === "despawn" ) {
							delete r.entities[e.gid];
							delete r.segments[e.gid];
						}
					}
				} );
			}
		};
	} );
	return instrumentation;
}
