/*
===========================================================================

flame.mjs - a flame graph of folded stacks, as one self-contained SVG

Usage:
  node tools/perf/analyze/flame.mjs FOLDED OUT.svg [--title TEXT] [--min PERCENT]

FOLDED is profile.mjs --folded output: "root;...;leaf weight" per line.
Each frame's width is its inclusive share of the total weight; frames
narrower than --min percent (default 0.05) are dropped so the file stays
readable. Hovering a frame shows its key and share (an SVG <title>).

===========================================================================
*/
import { readFileSync, writeFileSync } from "node:fs";
import { parseOptions } from "../core/report.mjs";

const USAGE = "flame.mjs FOLDED OUT.svg [--title TEXT] [--min PERCENT]";
const WIDTH = 1800;
const ROW = 17;
const FONT = 11;
const CHAR_WIDTH = 6.6;

/*
================
buildTree

The folded rows merged into one call tree: { name, weight, children }.
================
*/
function buildTree( text ) {
	const root = { name: "(all)", weight: 0, children: new Map() };
	for ( const line of text.split( "\n" ) ) {
		const at = line.lastIndexOf( " " );
		if ( at <= 0 ) continue;
		const weight = Number( line.slice( at + 1 ) );
		if ( !(weight > 0) ) continue;
		root.weight += weight;
		let node = root;
		for ( const name of line.slice( 0, at ).split( ";" ) ) {
			let child = node.children.get( name );
			if ( !child ) {
				child = { name, weight: 0, children: new Map() };
				node.children.set( name, child );
			}
			child.weight += weight;
			node = child;
		}
	}
	return root;
}

/*
================
hue

A stable warm colour per file, so one module's frames read as one band.
================
*/
function hue( name ) {
	const file = name.split( " " )[1]?.split( ":" )[0] ?? name;
	let hash = 0;
	for ( let i = 0; i < file.length; i++ ) hash = (hash * 31 + file.charCodeAt( i )) >>> 0;
	return `hsl(${hash % 50 + 5},${70 + hash % 20}%,${55 + (hash >> 8) % 15}%)`;
}

/*
================
escape
================
*/
function escape( text ) {
	return text.replace( /[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c] );
}

/*
================
render

Root at the bottom, callees stacked above, children widest first.
================
*/
function render( root, title, minShare ) {
	const rects = [];
	let depth = 0;
	const stack = [ { node: root, x: 0, level: 0 } ];
	while ( stack.length ) {
		const { node, x, level } = stack.pop();
		const width = node.weight / root.weight * WIDTH;
		depth = Math.max( depth, level );
		rects.push( { node, x, width, level } );
		let next = x;
		for ( const child of [ ...node.children.values() ].sort( ( a, b ) => b.weight - a.weight ) ) {
			if ( child.weight / root.weight * 100 >= minShare ) {
				stack.push( { node: child, x: next, level: level + 1 } );
			}
			next += child.weight / root.weight * WIDTH;
		}
	}
	const height = (depth + 1) * ROW + 40;
	const out = [
		`<svg xmlns="http://www.w3.org/2000/svg" width="${WIDTH}" height="${height}" font-family="monospace" font-size="${FONT}">`,
		`<rect width="100%" height="100%" fill="#fdfaf5"/>`,
		`<text x="6" y="16" font-size="14">${escape( title )}</text>`
	];
	for ( const { node, x, width, level } of rects ) {
		const y = height - (level + 1) * ROW;
		const share = (node.weight / root.weight * 100).toFixed( 2 );
		const label = width > 3 * CHAR_WIDTH ? node.name.slice( 0, Math.floor( (width - 4) / CHAR_WIDTH ) ) : "";
		out.push(
			`<g><title>${escape( node.name )} - ${share}%</title>` +
				`<rect x="${x.toFixed( 1 )}" y="${y}" width="${Math.max( 0.3, width - 0.5 ).toFixed( 1 )}" ` +
				`height="${ROW - 1}" fill="${hue( node.name )}"/>` +
				(label ? `<text x="${(x + 2).toFixed( 1 )}" y="${y + ROW - 5}">${escape( label )}</text>` : "") +
				"</g>"
		);
	}
	out.push( "</svg>" );
	return out.join( "\n" );
}

const options = parseOptions( process.argv.slice( 2 ), { title: "", min: 0.05 }, USAGE );
const [input, output] = options.files;
if ( !input || !output ) throw Error( `usage: ${USAGE}` );
writeFileSync( output, render( buildTree( readFileSync( input, "utf8" ) ), options.title || input, options.min ) );
