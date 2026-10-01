import { test } from "node:test";
import assert from "node:assert/strict";
import { createFrameProfiler } from "../../tools/lib/frame-profiler.mjs";
import { analyzeFrames, distribution } from "../../tools/lib/frame-analysis.mjs";
import { defined } from "../helpers/defined.mjs";
test("analysis joins GPU by CPU identity, deduplicates publications and does not add nested stages", () => {
	let t = 0;
	const owner = createFrameProfiler( () => t );
	owner.start();
	for ( let id = 1; id <= 3; id++ ) {
		owner.begin( id );
		t++;
		owner.mark( "character-presentation" );
		owner.renderBegin();
		t += id;
		owner.renderMark( "world-prepare" );
		t++;
		owner.renderMark( "character-prepare" );
		owner.mark( "render-preparation-submit" );
		t += .5;
		owner.end();
	}
	const sample = { frameId: 2, sequence: 500, passes: [ { name: "main", ms: .4 } ] },
		w = {
			telemetry: [ { sample: { gpu: { samples: [ sample ] } } } ],
			gpu: { samples: [ sample, { frameId: 999, passes: [ { ms: 100 } ] } ] },
			intervals: [ 4, 5, 6 ]
		};
	owner.frameDraw(
		3,
		undefined,
		undefined,
		[ { indexCount: 12, instanceCount: 2 } ],
		[ { count: 3 } ],
		[],
		undefined,
		undefined,
		undefined,
		undefined,
		[]
	);
	const capture = owner.stop(), result = analyzeFrames( capture, w );
	assert.equal( defined( defined( result.drawWork ).mainTriangles ).mean, 8 );
	assert.equal( defined( defined( result.drawWork ).uiQuads ).mean, 3 );
	assert.equal( defined( result.drawWork ).zeroDraws, 0 );
	const backwards = capture.rows.map( row => row.slice() );
	backwards[0][capture.columns.indexOf( "startMs" )] = 100;
	assert.throws( () => analyzeFrames( { ...capture, rows: backwards }, w ), /Nonmonotonic/ );
	assert.equal( defined( result.cpu ).mean, 4.5 );
	assert.equal( result.callbackBudgetExceeded, 2 );
	assert.equal( result.gpu.matchedFrames, 1 );
	assert.equal( defined( result.gpu.passSumMs ).mean, .4 );
	assert.equal( defined( result.callbackRemainder ).mean, .5 );
	assert.equal( result.slowFrameRanking[0].name, "render-preparation-submit" );
	assert.equal( result.worstFrames[0].frameId, 3 );
	assert.equal( result.stages["world-prepare"].nested, true );
	assert.throws( () => analyzeFrames( { ...capture, rows: [ capture.rows[0], capture.rows[0] ] }, w ), /Duplicate/ );
	assert.equal( distribution( [] ), null );
	assert.throws( () => distribution( [ NaN ] ), /Invalid/ );
	assert.throws( () => analyzeFrames( { ...capture, rows: [] }, w ), /No CPU frames/ );
	assert.throws( () => analyzeFrames( { ...capture, rows: [ [ NaN ] ] }, w ), /Invalid CPU/ );
	assert.ok( result.evidenceIssues.some( issue => issue.includes( "GPU coverage" ) ) );
	assert.ok(
		analyzeFrames( { ...capture, dropped: 1 }, w ).evidenceIssues.some( issue => issue.includes( "capacity" ) )
	);
});

test("sampled inner costs exclude unmeasured frames instead of diluting their means", () => {
	let t = 0;
	const owner = createFrameProfiler( () => t );
	owner.start();
	for ( let id = 0; id < 64; id++ ) {
		owner.begin( id );
		if ( owner.sampleDetails() ) {
			owner.detailBegin( "world-instance-loop" );
			t += 2;
			owner.detailEnd( "world-instance-loop" );
		}
		t++;
		owner.end();
	}
	const result = analyzeFrames( owner.stop(), { intervals: [ 4 ] } );
	assert.equal( result.stages["world-instance-loop"].all.count, 2 );
	assert.equal( result.stages["world-instance-loop"].all.mean, 2 );
	assert.match( result.stages["world-instance-loop"].sampling, /includes timing overhead/ );
});
