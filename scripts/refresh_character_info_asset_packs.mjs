/*
===========================================================================

refresh_character_info_asset_packs.mjs - republish the native skill data

The characterInfo plane and the skill catalogue are published by one build
step as three files (skillData, skillAudioData, characterActionData);
rebuild them and repack each in the group that owns it. Files new to the
index join game-data, the startup group the full build puts them in.

===========================================================================
*/
import { buildSkillDataAsset } from "./build/data/buildSkillDataAsset.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

await withGeneratedAssetsLock( "Native characterInfo publication", async () => {
	await buildSkillDataAsset();
	await publishLooseFamily( {
		name: "character-info",
		files: [
			"/assets/data/skillData.json.gz",
			"/assets/data/skillAudioData.json.gz",
			"/assets/data/characterActionData.json.gz"
		],
		defaultGroup: "game-data"
	} );
} );
