/*
===========================================================================

guild-notice-hud.ts - the embedded Guild notice reader

5E3090 mode 2 replaces the members and commands with NotifyContents.
5F3D70 starts at line zero; Confirm (5F3E20) returns to mode 1.

===========================================================================
*/

/*
================
createGuildNoticeHud
================
*/
export function createGuildNoticeHud() {
	let open = false, offset = 0, range = 0;
	return {
		/*
		================
		open
		================
		*/
		open() {
			open = true;
			offset = 0;
		},
		/*
		================
		close
		================
		*/
		close() {
			open = false;
			offset = 0;
			range = 0;
		},
		/*
		================
		isOpen
		================
		*/
		isOpen() {
			return open;
		},
		/*
		================
		position
		================
		*/
		position( count: number, visible: number ) {
			range = Math.max( 0, count - visible );
			offset = Math.max( 0, Math.min( range, offset ) );
			return Math.floor( offset );
		},
		/*
		================
		scroll
		================
		*/
		scroll( delta: number ) {
			offset = Math.max( 0, Math.min( range, offset + delta ) );
		},
		/*
		================
		drag
		================
		*/
		drag( delta: number, travel: number ) {
			if ( travel > 0 ) offset = Math.max( 0, Math.min( range, offset + delta * range / travel ) );
		}
	};
}
