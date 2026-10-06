/*
===========================================================================

nav-replay.mjs - replay server probe moves through the client's clipper

The client predicts every click-to-move with its own navigation port; when
it and the server disagree, a player walks into a place the server refuses
(and snaps back) or cannot walk where the server would let them. This tool
replays the moves `sro-evidence navsweep -moves` wrote, with the server's
verdicts, through the navigation modules the client ships, and reports each
move whose resting point differs by more than a tolerance.

Usage (from apps/client-next):

	node tools/nav-replay.mjs <moves.json> [--public <client-public root>] [--tolerance 1]

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../scripts/lib/generatedRoot.mjs";
import "../tests/helpers/native-source-loader.mjs";
import { readFileSync } from "node:fs";
import path from "node:path";
import { readPublishedAssetBytesSync } from "../../../scripts/lib/publishedAsset.mjs";

const { createNavigation } = await import(
	"../src/engine/runtime/simulation/worker/session/world/gameplay/movement/navigation/navigation.ts"
);
const { createNavigationResources } = await import( "../src/engine/runtime/assets/worker/navigation/navigation.ts" );

const args = process.argv.slice( 2 );
const option = ( name, fallback ) => {
	const at = args.indexOf( name );
	return at >= 0 ? args[at + 1] : fallback;
};
const movesPath = args.find( ( arg ) =>
	!arg.startsWith( "--" ) && args[args.indexOf( arg ) - 1]?.startsWith( "--" ) !== true
);
const publicRoot = path.resolve( option( "--public", CLIENT_PUBLIC_ROOT ) );
const tolerance = Number( option( "--tolerance", "1" ) );
if ( !movesPath ) throw new Error( "usage: node tools/nav-replay.mjs <moves.json> [--public <root>] [--tolerance 1]" );

const moves = JSON.parse( readFileSync( movesPath, "utf8" ) );
const bytes = ( publicPath ) => readPublishedAssetBytesSync( publicPath, publicRoot );
const hex = ( region ) => region.toString( 16 ).padStart( 4, "0" );
// Install the region most moves start in: the sweep's own region.
const counts = new Map();
for ( const move of moves ) counts.set( move.from.regionWord, (counts.get( move.from.regionWord ) ?? 0) + 1 );
const origin = [ ...counts ].sort( ( a, b ) => b[1] - a[1] )[0][0];

/*
================
installRegion

Installs the origin region's navigation product, built exactly as the
client's asset worker builds it.
================
*/
async function installRegion( navigation, region ) {
	const product = await createNavigationResources().resolve(
		bytes( `/assets/world/outdoor/regions/region-${hex( region )}.json` ),
		region,
		async ( publicPath ) => bytes( publicPath )
	);
	navigation.install( region, product );
}

/*
================
pose
================
*/
function pose( point ) {
	return { regionId: point.regionWord, x: point.x, y: point.y, z: point.z, angle: 0 };
}

/*
================
worldDistance

Distance between two rest points in world units, across region borders.
================
*/
function worldDistance( a, b ) {
	return Math.hypot( (a.worldX - b.worldX) * 10, (a.worldY - b.worldY) * 10 );
}

const navigation = createNavigation();
await installRegion( navigation, origin );

// Agreement is judged by where each side comes to rest; a server "blocked"
// that stops a hair short of the goal agrees with a client that arrives.
const disagreements = { clientStopsShort: [], clientGoesFurther: [] };
for ( const move of moves ) {
	const rest = navigation.clip( pose( move.from ), pose( move.goal ) );
	const clientRest = rest ?
		{
			worldX: (((rest.regionId & 255) - 135) * 192) + rest.x / 10,
			worldY: (((rest.regionId >>> 8) - 92) * 192) + rest.z / 10
		} :
		move.from;
	const row = {
		from: [ move.from.worldX, move.from.worldY ],
		goal: [ move.goal.worldX, move.goal.worldY ],
		server: move.outcome,
		serverRest: [ move.rest.worldX, move.rest.worldY ],
		clientRest: [ clientRest.worldX, clientRest.worldY ]
	};
	if ( worldDistance( clientRest, move.rest ) <= tolerance ) continue;
	const clientShort = worldDistance( clientRest, move.goal ) > worldDistance( move.rest, move.goal );
	disagreements[clientShort ? "clientStopsShort" : "clientGoesFurther"].push( row );
}

console.log( `nav-replay: ${moves.length} moves in region 0x${hex( origin )}` );
for ( const [kind, rows] of Object.entries( disagreements ) ) {
	console.log( `  ${kind}: ${rows.length}` );
	for ( const row of rows.slice( 0, 8 ) ) console.log( `    ${JSON.stringify( row )}` );
}
