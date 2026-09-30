/*
===========================================================================

publish-weather-assets.mjs - NPC rain-event flags and weather sounds

Marks each NPC model that plays the rain event (skilleffect.txt) in the NPC
manifest and publishes the weather sounds, outside the full build. Patches
only the affected pack groups and republishes the web manifest.

===========================================================================
*/
import fs from "node:fs/promises";
import path from "node:path";
import { parseWeatherEvents } from "../../../scripts/build/char/weatherEvents.mjs";
import { buildWeatherSoundResources } from "../../../scripts/build/shared/audioResources.mjs";
import { withGeneratedAssetsLock } from "../../../scripts/rebuildLock.mjs";
import { refreshPrecompressedSidecars } from "../../../scripts/build/generatedManifestSidecars.mjs";
import { patchAssetPackGroupFromLooseFiles } from "../../../scripts/build/sparseAssetPackGroupRefresh.mjs";
import { buildWebAssetManifest } from "../../../scripts/build/webManifest.mjs";
import { publishBytesAtomically } from "../../../scripts/build/shared/atomicPublish.mjs";
import { retailTextdataRoot } from "../../../scripts/build/world/paths.mjs";
const root = path.resolve( import.meta.dirname, "../../.." ),
	publicRoot = path.join( root, ".generated/client-public" );
await withGeneratedAssetsLock( "native weather publication", async () => {
	const flags = parseWeatherEvents(
		await fs.readFile( path.join( retailTextdataRoot, "skilleffect.txt" ), "utf16le" )
	);
	const npcPath = path.join( publicRoot, "assets/npc/manifest.json" ),
		npc = JSON.parse( await fs.readFile( npcPath, "utf8" ) );
	for ( const row of Object.values( npc.models ) ) row.eventRain = flags.get( row.codename ) ?? false;
	await publishBytesAtomically( npcPath, Buffer.from( JSON.stringify( npc ) ), {
		logLabel: "weather event metadata"
	} );
	await refreshPrecompressedSidecars( [ npcPath ], { onlyWhenStale: true } );
	const sounds = await buildWeatherSoundResources(),
		packPath = path.join( publicRoot, "assets/packs/manifest.json" ),
		previous = JSON.parse( await fs.readFile( packPath, "utf8" ) );
	const deltas = new Map();
	for ( const file of [ "/assets/npc/manifest.json.gz", ...sounds ] ) {
		const group = previous.assets.find( row => row.path === file )?.group ??
			(file.endsWith( ".wav" ) ? "game-audio" : null);
		if ( !group || !previous.groups.some( row => row.name === group ) ) throw Error( "No pack owner for " + file );
		const files = deltas.get( group ) ?? [];
		files.push( file );
		deltas.set( group, files );
	}
	const updates = [];
	for ( const [groupName, looseFiles] of deltas ) {
		updates.push(
			await patchAssetPackGroupFromLooseFiles( {
				publicRoot,
				outputRoot: path.join( publicRoot, "assets/packs/incremental/weather", groupName ),
				previousIndex: previous,
				groupName,
				looseFiles
			} )
		);
	}
	const merged = {
		...previous,
		groups: [ ...previous.groups.filter( row => !deltas.has( row.name ) ), ...updates.flatMap( row => row.groups ) ]
			.sort( ( a, b ) => a.name.localeCompare( b.name ) ),
		assets: [
			...previous.assets.filter( row => !deltas.has( row.group ) ),
			...updates.flatMap( row => row.assets )
		].sort( ( a, b ) => a.path.localeCompare( b.path ) )
	};
	await publishBytesAtomically( packPath, Buffer.from( JSON.stringify( merged ) ), {
		logLabel: "weather pack manifest"
	} );
	await buildWebAssetManifest();
	await refreshPrecompressedSidecars( [ packPath, path.join( publicRoot, "assets/manifest.json" ) ], {
		onlyWhenStale: true
	} );
	console.log( JSON.stringify( { flagged: [ ...flags ].filter( row => row[1] ).map( row => row[0] ), sounds } ) );
} );
