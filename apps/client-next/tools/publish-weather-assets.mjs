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
import { refreshPrecompressedSidecars } from "../../../scripts/build/generatedManifestSidecars.mjs";
import { publishBytesAtomically } from "../../../scripts/build/shared/atomicPublish.mjs";
import { buildWeatherSoundResources } from "../../../scripts/build/shared/audioResources.mjs";
import { publishLooseFamily } from "../../../scripts/build/shared/looseFamilyPublication.mjs";
import { publicRoot, retailTextdataRoot } from "../../../scripts/build/world/paths.mjs";
import { withGeneratedAssetsLock } from "../../../scripts/rebuildLock.mjs";

await withGeneratedAssetsLock( "native weather publication", async () => {
	const flags = parseWeatherEvents(
		await fs.readFile( path.join( retailTextdataRoot, "skilleffect.txt" ), "utf16le" )
	);
	const npcPath = path.join( publicRoot, "assets", "npc", "manifest.json" );
	const npc = JSON.parse( await fs.readFile( npcPath, "utf8" ) );
	for ( const row of Object.values( npc.models ) ) row.eventRain = flags.get( row.codename ) ?? false;
	await publishBytesAtomically( npcPath, Buffer.from( JSON.stringify( npc ) ), {
		logLabel: "weather event metadata"
	} );
	await refreshPrecompressedSidecars( [ npcPath ], { onlyWhenStale: true } );
	const sounds = await buildWeatherSoundResources();
	// The NPC manifest keeps its owner; a new weather sound joins game-audio.
	await publishLooseFamily( {
		name: "weather",
		files: [ "/assets/npc/manifest.json.gz", ...sounds ],
		defaultGroup: file => file.endsWith( ".wav" ) ? "game-audio" : undefined
	} );
	console.log( JSON.stringify( { flagged: [ ...flags ].filter( row => row[1] ).map( row => row[0] ), sounds } ) );
} );
