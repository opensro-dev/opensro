/*
===========================================================================

shown-gameplay.mjs - the gameplay state a reader holds after each batch

A gameplay publication names only the fields that changed since the one
before it (foundation/gameplay/gameplay-snapshot.ts), so a test that
asks what the player is shown folds every batch the way presentation
does: a reset drops the state, each gameplay event merges into it. The
merge is passed in because tests load the source through different
loaders.

===========================================================================
*/

/*
================
createShownGameplay

Returns fold( events ): apply one batch's events and return the gameplay
state shown after it (null before the first publication).
================
*/
export function createShownGameplay( mergeGameplaySnapshots ) {
	let shown = null;
	return events => {
		for ( const event of events ?? [] ) {
			if ( event.kind === "reset" ) shown = null;
			else if ( event.kind === "gameplay" ) shown = mergeGameplaySnapshots( shown, event.state, event.delta );
		}
		return shown;
	};
}
