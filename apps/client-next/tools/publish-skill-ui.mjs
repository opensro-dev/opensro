/*
===========================================================================

publish-skill-ui.mjs - the native skill window projection

build-skill-ui.py projects the skill window data into assets/data/
skillUi.json, outside the full build. It is packed beside the skill mastery
data it is read with, unless a group already owns it.

===========================================================================
*/
import path from "node:path";
import { refreshPrecompressedSidecars } from "../../../scripts/build/generatedManifestSidecars.mjs";
import { publishLooseFamily } from "../../../scripts/build/shared/looseFamilyPublication.mjs";
import { runPython } from "../../../scripts/build/shared/pythonRun.mjs";
import { publicRoot } from "../../../scripts/build/world/paths.mjs";
import { withGeneratedAssetsLock } from "../../../scripts/rebuildLock.mjs";

const MASTERY_DATA = "/assets/data/skillmasterydata.json.gz";

await withGeneratedAssetsLock( "native skill UI publication", async () => {
	await runPython( [ path.join( import.meta.dirname, "build-skill-ui.py" ) ], {
		task: "Native skill UI projection"
	} );
	await refreshPrecompressedSidecars( [ path.join( publicRoot, "assets", "data", "skillUi.json" ) ], {
		onlyWhenStale: true
	} );
	await publishLooseFamily( {
		name: "skill-ui",
		owner: "skill-ui",
		files: [ "/assets/data/skillUi.json.gz" ],
		defaultGroup: ( file, previous ) =>
			previous.assets.find( row => row.path.toLowerCase() === MASTERY_DATA )?.group
	} );
} );
