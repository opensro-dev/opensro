/*
===========================================================================

looseFamilyPublication.mjs - publish a small family of loose files into packs

Focused publishers (footprints, overlays, quick status, quickslots, return
scrolls) write a handful of loose files into the public tree and must then
pack them without disturbing anything else. This owner does that one way:
each file joins the group that already holds it (or the family's default
group), and only those groups are republished through packGroupRefresh.mjs
(the one refresh sequence: merge with its schema guard, closure check,
superseded packs soft-archived, web manifest). The family's files are then
recorded as its publication-ledger owner.

The caller holds the generated-assets lock.

===========================================================================
*/
import { refreshOwnedPackFiles } from "./ownedPackRefresh.mjs";
import { beginPublication, claimPublicPaths, commitPublication, isPublicationOpen } from "./publicationLedger.mjs";

/*
================
publishLooseFamily

family.name names the incremental pack folder and the log label;
family.files are public paths already written loose; family.defaultGroup
(a group name, or a function of the path and the previous index) places
files no group holds yet.
Without a default every file must already have an owner. Returns the
patched group updates so a caller can report what was rebuilt.
================
*/
export async function publishLooseFamily( family ) {
	// The ledger owner is the family's task name (assets:refresh|publish:<owner>),
	// so the build can tell a family that never ran from one that ran.
	if ( !family.owner ) throw new Error( `${family.name}: publishLooseFamily needs the family's task name as owner` );
	const { updates } = await refreshOwnedPackFiles( {
		name: family.name,
		files: family.files,
		defaultGroup: family.defaultGroup,
		// Families keep the full-strength manifest sidecars they always published.
		manifestSidecars: {}
	} );
	// The family's files are its whole claim (publicationLedger.mjs). Inside a
	// larger open publication they join that owner instead.
	if ( isPublicationOpen() ) claimPublicPaths( family.files );
	else {
		beginPublication( `family-${family.owner}` );
		claimPublicPaths( family.files );
		await commitPublication();
	}
	return updates;
}
