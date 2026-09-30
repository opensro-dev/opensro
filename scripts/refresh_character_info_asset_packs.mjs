/*
===========================================================================

refresh_character_info_asset_packs.mjs - republish the native skill data

The characterInfo window reads skillData; rebuild it and repack it in the
group that already owns it.

===========================================================================
*/
import { buildSkillDataAsset } from "./build/data/buildSkillDataAsset.mjs";
import { publishLooseFamily } from "./build/shared/looseFamilyPublication.mjs";
import { withGeneratedAssetsLock } from "./rebuildLock.mjs";

await withGeneratedAssetsLock( "Native characterInfo publication", async () => {
	await buildSkillDataAsset();
	await publishLooseFamily( { name: "character-info", files: [ "/assets/data/skillData.json.gz" ] } );
} );
