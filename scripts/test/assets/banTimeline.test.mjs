import { CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
import { compileBanTimeline } from "../../build/char/compileBanTimeline.mjs";
import { avatarToGlb } from "../../build/char/exportGlb.mjs";
import { parseBan, parseCharacterBsr } from "../../build/char/formats.mjs";
import { loadDataAsset } from "../../build/shared/jmxAssetIO.mjs";
import { readPublishedAssetBytesSync } from "../../lib/publishedAsset.mjs";
import { fileURLToPath } from "node:url";

function clip( times ) {
	return {
		frameCount: times.length,
		frameTimesMs: times,
		bones: [ "root", "child" ].map( name => ({
			name,
			keyCount: times.length,
			keys: times.map( ( _, i ) => ({
				q: [ 0, Math.sin( i / 4 ), 0, Math.cos( i / 4 ) ],
				t: [ i, 2 * i, 3 * i ]
			}) )
		}) )
	};
}
function exportClip( value ) {
	return avatarToGlb( {
		name: "timeline-fixture",
		parts: [],
		materials: new Map(),
		clips: [ { role: "stand", clip: value } ],
		skeleton: {
			boneCount: 2,
			byName: new Map( [ [ "root", 0 ], [ "child", 1 ] ] ),
			bones: [ { name: "root", parentIndex: -1, local: { q: [ 0, 0, 0, 1 ], t: [ 0, 0, 0 ] } }, {
				name: "child",
				parentIndex: 0,
				local: { q: [ 0, 0, 0, 1 ], t: [ 0, 0, 0 ] }
			} ]
		}
	} );
}
function read( glb ) {
	const size = glb.readUInt32LE( 12 ),
		json = JSON.parse( glb.subarray( 20, 20 + size ) ),
		binary = glb.subarray( 28 + size );
	return {
		json,
		values( index ) {
			const a = json.accessors[index],
				v = json.bufferViews[a.bufferView],
				width = { SCALAR: 1, VEC3: 3, VEC4: 4 }[a.type],
				start = (v.byteOffset ?? 0) + (a.byteOffset ?? 0);
			return Array.from( { length: a.count * width }, ( _, i ) => binary.readFloatLE( start + i * 4 ) );
		}
	};
}
test("BAN timestamp map keeps first insertion and sorts native keys", () => {
	const source = clip( [ 1000, 0, 1000, 500, 500, 2000 ] );
	const timeline = compileBanTimeline( source );
	assert.deepEqual( timeline.sourceIndices, [ 1, 3, 0, 5 ] );
	assert.deepEqual( [ ...timeline.times ], [ 0, 0.5, 1, 2 ] );
	assert.deepEqual( source.frameTimesMs, [ 1000, 0, 1000, 500, 500, 2000 ] );
});
test("exported channels select the same first conflicting key and preserve subsequent keys", () => {
	const { json, values } = read( exportClip( clip( [ 0, 1000, 1000, 2000 ] ) ) );
	for ( const channel of json.animations[0].channels ) {
		const sampler = json.animations[0].samplers[channel.sampler];
		assert.deepEqual( values( sampler.input ), [ 0, 1, 2 ] );
		assert.equal( json.accessors[sampler.output].count, 3 );
		if ( channel.target.path === "rotation" ) {
			assert.deepEqual(
				values( sampler.output ),
				[ 0, 1, 3 ].flatMap(
					i => [ -0, Math.fround( -Math.sin( i / 4 ) ), 0, Math.fround( Math.cos( i / 4 ) ) ]
				)
			);
		}
		if ( channel.target.path === "translation" ) {
			assert.deepEqual( values( sampler.output ), [ 0, 0, -0, 1, 2, -3, 3, 6, -9 ] );
		}
	}
});
test("unique timelines and single-key clips retain their original samples", () => {
	for ( const times of [ [ 0 ], [ 0, 500, 1000 ] ] ) {
		assert.deepEqual(
			compileBanTimeline( clip( times ) ).sourceIndices,
			times.map( ( _, i ) => i )
		);
	}
	assert.equal( read( exportClip( clip( [ 0, 0, 0 ] ) ) ).json.accessors.at( -1 ).count, 1 );
});
test("float32 collisions and malformed frame/track counts fail before export", () => {
	assert.throws( () => exportClip( clip( [ 4000000000, 4000000001 ] ) ), /collide/ );
	const bad = clip( [ 0, 1000 ] );
	bad.bones[1].keys.pop();
	assert.throws( () => exportClip( bad ), /key count/ );
	assert.throws( () => compileBanTimeline( clip( [ -1, 1 ] ) ), /timestamp/ );
	assert.throws( () => compileBanTimeline( clip( [] ) ), /frame count/ );
});

test("published Bugghost conflicting keys match first native BAN samples in every channel", async () => {
	const bsr = parseCharacterBsr( await loadDataAsset( "res/mob/dunhuang/bugghost.bsr" ) );
	const state = bsr.animationSets.find( set => set.name.toLowerCase() === "default" ).states.find( state =>
		state.stateId === 17
	);
	const source = parseBan( await loadDataAsset( state.animationPath ) );
	const unique = [ ...new Set( source.frameTimesMs ) ].sort( ( a, b ) => a - b );
	assert.ok( unique.length < source.frameCount, "fixture must retain its conflicting native timestamps" );
	const publicRoot = CLIENT_PUBLIC_ROOT + "/";
	const { json, values } = read( readPublishedAssetBytesSync( "/assets/npc/mob/dunhuang/bugghost.glb", publicRoot ) );
	const animation = json.animations.find( clip => clip.name === "attack4" );
	for ( const channel of animation.channels ) {
		const bone = source.bones.find( bone => bone.name === json.nodes[channel.target.node].name );
		assert.ok( bone );
		const sampler = animation.samplers[channel.sampler];
		assert.deepEqual( values( sampler.input ), unique.map( time => Math.fround( time / 1000 ) ) );
		const expected = unique.flatMap( time => {
			const key = bone.keys[source.frameTimesMs.indexOf( time )];
			return channel.target.path === "rotation" ?
				[ -key.q[0], -key.q[1], key.q[2], key.q[3] ] :
				[ key.t[0], key.t[1], -key.t[2] ];
		} );
		assert.deepEqual( values( sampler.output ), expected );
	}
});
