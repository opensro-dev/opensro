import { normalizeAssetPath } from "./assetPaths.mjs";

/**
 * Collect both authored skill-mastery sprites from every valid
 * skillmasterydata.txt row. Columns 11 and 12 are independent native paths;
 * neither is inferred from the other.
 */
export function collectSkillMasteryIconDdjReferencesFromRows( rows ) {
	const references = new Set();

	for ( const line of rows ) {
		if ( !line.trim() || line.trimStart().startsWith( "//" ) ) {
			continue;
		}
		const columns = line.split( "\t" );
		for ( const columnIndex of [ 11, 12 ] ) {
			const ddjPath = normalizeAssetPath( columns[columnIndex] );
			if ( ddjPath.endsWith( ".ddj" ) ) {
				references.add( ddjPath );
			}
		}
	}
	return [ ...references ].sort();
}

/**
 * Collect the two resources reached by CIFSkillGroup_BuildSlotCells for each
 * skillgroup.txt row. Native applies column 6 as the primary sprite, then for
 * paths longer than four bytes strips the extension and appends
 * "_focus.ddj" for the secondary CTextBoard sprite.
 */
export function collectSkillGroupIconDdjReferencesFromRows( rows ) {
	const references = new Set();

	for ( const line of rows ) {
		if ( !line.trim() || line.trimStart().startsWith( "//" ) ) {
			continue;
		}
		const sourcePath = normalizeAssetPath( line.split( "\t" )[6] );
		if ( !sourcePath.endsWith( ".ddj" ) ) {
			continue;
		}
		const primaryPath = normalizeAssetPath( `icon/${sourcePath}` );
		references.add( primaryPath );
		if ( sourcePath.length > 4 ) {
			references.add(
				normalizeAssetPath( `icon/${sourcePath.slice( 0, -4 )}_focus.ddj` )
			);
		}
	}
	return [ ...references ].sort();
}
