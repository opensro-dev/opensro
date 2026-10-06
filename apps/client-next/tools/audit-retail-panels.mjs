/*
===========================================================================

audit-retail-panels.mjs - published panel layouts against the retail resinfo

An independent resinfo comparison, not a snapshot of the current UI output:
it discovers the requested panel families and every control and section in
each file, then checks the published layouts field by field. It certifies
publication fields only; a resource match is not raster or branch parity.
Never change those statuses merely because this audit passes.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../scripts/lib/generatedRoot.mjs";
import { readFile, readdir, mkdir, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";
import path from "node:path";
import assert from "node:assert/strict";
import { applyCifPreprocessor, loadCifDefines } from "../../../scripts/build/shared/cifPreprocessor.mjs";
import { clientV150ResinfoRoot } from "../../../scripts/build/world/paths.mjs";

const app = path.resolve( path.dirname( fileURLToPath( import.meta.url ) ), ".." );
const retail = clientV150ResinfoRoot;
const published = CLIENT_PUBLIC_ROOT + "/assets";
const hash = bytes => createHash( "sha256" ).update( bytes ).digest( "hex" );
const names = (await readdir( retail )).filter( name =>
	/^if(?:_npc(?:talk|window)|quest\w*|store|gameguide|ggmenu|mainpopup|messagebox)\.txt$/i.test( name )
).sort();
const fontBytes = await readFile( path.join( published, "fonts/native-ui-font-atlas.json" ) );
const atlas = JSON.parse( fontBytes ), panels = [];
const defines = await loadCifDefines();
const defineBytes = await readFile( path.resolve( retail, "../config/define.txt" ) );
for ( const name of names ) {
	const bytes = await readFile( path.join( retail, name ) );
	const text = applyCifPreprocessor(
		bytes.toString( bytes[0] === 255 && bytes[1] === 254 ? "utf16le" : "utf8" ),
		defines
	);
	const jsonBytes = await readFile( path.join( published, "cif/layouts", name.replace( /\.txt$/, ".json" ) ) );
	const json = JSON.parse( jsonBytes ),
		nodes = json.sections.flatMap( section => section.nodes.map( node => ({ section: section.name, node }) ) );
	const source = [ ...text.matchAll( /(\w+)\s*:\s*(\w+)\s*\{([^{}]*)\}/g ) ];
	assert.equal( source.length, nodes.length, `${name}: section/control inventory drift` );
	const controls = source.map( ( match, i ) => {
		const { section, node } = nodes[i];
		assert.equal( node.name, match[1], `${name}: control order` );
		assert.equal( node.type, match[2], `${name}: control type` );
		const properties = Object.fromEntries(
			[ ...match[3].matchAll( /(\w+)\s*=\s*(\w+)\s*,\s*"([^"\r\n]*)"/g ) ].map( m => [ m[1], m[3] ] )
		);
		const list = key => {
			assert.ok( properties[key] !== undefined, `${name}/${node.name}: missing ${key}` );
			return properties[key].split( "," ).map( Number );
		};
		const rect = value => [ value.x, value.y, value.width, value.height ];
		assert.deepEqual( rect( node.rect ), list( "Rect" ), `${name}/${node.name}: position/extent` );
		assert.deepEqual( rect( node.clientRect ), list( "ClientRect" ), `${name}/${node.name}: text insets` );
		for ( const [key, field] of [ [ "FontIndex", "fontIndex" ], [ "HAlign", "hAlign" ], [ "VAlign", "vAlign" ] ] ) {
			assert.equal( node[field], Number( properties[key] ), `${name}/${node.name}: ${key}` );
		}
		assert.deepEqual(
			[ node.fontColor.a, node.fontColor.r, node.fontColor.g, node.fontColor.b ],
			list( "FontColor" ),
			`${name}/${node.name}: font color`
		);
		assert.equal( node.text ?? "", properties.Text, `${name}/${node.name}: text symbol` );
		assert.ok( atlas.fonts[node.fontIndex], `${name}/${node.name}: absent bitmap font` );
		return {
			section,
			name: node.name,
			type: node.type,
			rect: rect( node.rect ),
			client: rect( node.clientRect ),
			font: node.fontIndex,
			align: [ node.hAlign, node.vAlign ],
			text: node.text ?? "",
			color: list( "FontColor" )
		};
	} );
	panels.push( {
		name,
		sourceSha256: hash( bytes ),
		publicationSha256: hash( jsonBytes ),
		authoredFields: "matched",
		raster: "unverified",
		interactionBranches: "requires-native-and-live-evidence",
		controls
	} );
}
assert.ok( panels.length > 0, "No retail panel resources discovered" );
const out = path.join( app, "temp/artifacts/retail-panels" );
await mkdir( out, { recursive: true } );
await writeFile(
	path.join( out, "authored-audit.json" ),
	JSON.stringify(
		{
			scope: "NPC conversation, quest, guide, merchant and shared confirmation/popup resources",
			defineSha256: hash( defineBytes ),
			fontAtlasSha256: hash( fontBytes ),
			fontSlots: Object.fromEntries(
				Object.entries( atlas.fonts ).map( (
					[key, font]
				) => [ key, { recordHeight: font.recordHeight, ascent: font.ascent, descent: font.descent } ] )
			),
			panels
		},
		null,
		2
	)
);
console.log(
	`Matched ${panels.length} retail resources / ${
		panels.reduce( ( n, p ) => n + p.controls.length, 0 )
	} controls. Raster and branch parity remain separately qualified.`
);
