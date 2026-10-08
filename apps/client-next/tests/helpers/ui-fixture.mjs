/*
===========================================================================

ui-fixture.mjs - production HUD with synchronous retail asset delivery

Shared behavioral fixture. Every published control list must have unique
identities, including panels exercised by otherwise unrelated UI tests.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "./native-source-loader.mjs";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { createUi } = await import( "../../src/engine/runtime/ui/ui.ts" );
const { expandTextRuns } = await import( "../../src/engine/foundation/rendering/text-run.ts" );
export const fontAtlas = JSON.parse(
	readFileSync( CLIENT_PUBLIC_ROOT + "/assets/fonts/native-ui-font-atlas.json", "utf-8" )
);
/*
================
uiFixture
================
*/
export function uiFixture(
	commands = () => {},
	hold = ( _path ) => false,
	audioPreference = () => {},
	saveSight = () => {},
	saveBindings = () => {},
	saveVideo = () => {},
	saveOptions = () => {},
	extensions = {}
) {
	const scenes = [], products = [], textures = [], sounds = [], requested = [], pending = new Map();
	let nextId = 0;
	const ui = createUi(
		{
			available: () => 8,
			take: id => {
				const bytes = pending.get( id );
				if ( bytes?.path && hold( bytes.path ) ) return null;
				pending.delete( id );
				return bytes?.image ?
					{ kind: "image", id, image: bytes.image } :
					bytes ?
					{
						kind: "bytes",
						id,
						buffer: bytes.buffer.slice( bytes.byteOffset, bytes.byteOffset + bytes.byteLength )
					} :
					null;
			},
			request: url => {
				const id = ++nextId, path = decodeURIComponent( new URL( url ).pathname );
				requested.push( path );
				try {
					const bytes = readFileSync( CLIENT_PUBLIC_ROOT + path );
					if ( path.endsWith( ".json" ) || path.endsWith( ".txt" ) ) pending.set( id, bytes );
					else if ( path.endsWith( ".png" ) ) {
						pending.set( id, {
							path,
							image: {
								width: bytes.readUInt32BE( 16 ),
								height: bytes.readUInt32BE( 20 ),
								/*
								================
								close
								================
								*/
								close() {}
							}
						} );
					}
				} catch {}
				return id;
			},
			cancel: id => pending.delete( id )
		},
		commands,
		// Scenes are recorded as drawn: text runs expanded into the glyph quads the
		// GPU packer writes (text-run.ts), so assertions read painted glyphs.
		/*
		================
		recordProduct
		================
		*/
		s => {
			products.push( s );
			scenes.push( s && { ...s, quads: expandTextRuns( s.quads ) } );
		},
		( ...args ) => textures.push( args ),
		"https://fixture.invalid/",
		"https://fixture.invalid/",
		undefined,
		kind => sounds.push( kind ),
		undefined,
		undefined,
		saveOptions,
		audioPreference,
		saveSight,
		saveBindings,
		saveVideo,
		undefined,
		undefined,
		extensions
	);
	const entity = { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0, kind: "player", name: "Player", mountedOn: 0 };
	const state = {
		session: { phase: "world", revision: 1, character: "Player" },
		gameplay: { localGid: 1, pose: { ...entity, angle: 0 }, vitals: [], inventory: [], target: 0 },
		entities: [ entity ],
		width: 1600,
		height: 900,
		worldReady: true
	};
	/*
	================
	hasText
	================
	*/
	function hasText( value, font = "0", style = 0 ) {
		const face = style === 2 ? fontAtlas.fonts[font].styles["2"] : fontAtlas.fonts[font];
		const pattern = Array.from( value, c => {
			const g = face.glyphs[c.codePointAt( 0 )] ?? face.glyphs["63"];
			return [
				g.x / fontAtlas.atlasWidth,
				g.y / fontAtlas.atlasHeight,
				g.width / fontAtlas.atlasWidth,
				g.height / fontAtlas.atlasHeight
			].join( "," );
		} );
		const actual =
			scenes.at( -1 )?.quads.filter( q => q.texture === fontAtlas.image ).map( q => q.uv.join( "," ) ) ?? [];
		return actual.some( ( _, start ) => pattern.every( ( uv, i ) => actual[start + i] === uv ) );
	}
	/*
	================
	checkedStep

	Check every intermediate publication, including cold asset admission.
	================
	*/
	function checkedStep( state, now ) {
		const result = ui.step( state, now );
		if ( result ) {
			const ids = result.controls.map( control => control.id );
			assert.equal(
				new Set( ids ).size,
				ids.length,
				"Duplicate UI controls: " + ids.filter( ( id, index ) => ids.indexOf( id ) !== index ).join( ", " )
			);
		}
		return result;
	}
	return {
		requested,
		sounds,
		rawStep: checkedStep,
		ui: {
			...ui,
			/*
			================
			step
			================
			*/
			step( state, now ) {
				let result = checkedStep( state, now );
				for ( let i = 0; i < 8 && pending.size; i++ ) result = checkedStep( state, now ) ?? result;
				// The skill catalogue decodes in bounded steps after its bytes arrive.
				for ( let i = 0; i < 64 && ui.stats().hudSettling; i++ ) result = checkedStep( state, now ) ?? result;
				return result;
			}
		},
		hasText,
		scenes,
		products,
		textures,
		state,
		/*
		================
		dispose
		================
		*/
		dispose() {
			ui.dispose();
		}
	};
}
