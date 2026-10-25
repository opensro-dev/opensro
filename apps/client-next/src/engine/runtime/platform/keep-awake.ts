/*
===========================================================================

keep-awake.ts - keeps a background game tab from being put to sleep

A native client keeps updating while its window is inactive
(Application_PumpIdleFrame A0F430 runs OnUpdate every idle frame), so its
connection always answers the server's keepalive. A browser may freeze a
background tab outright instead: Edge's sleeping tabs stop every script,
the simulation worker included, and the server detaches the silent session
after its idle timeout and ends it after the resume grace. The tab then
wakes to "Your session ended while the connection was gone".

Holding a Web Lock is one of the conditions Edge (and Chromium's own tab
freezing) documents as keeping a tab awake, so this owner holds one while
a world session is live and releases it otherwise. Timers in a background
tab are still throttled; only the freeze is avoided, which is what lets
the worker keep answering pings. Browsers without the Web Locks API keep
today's behaviour.

===========================================================================
*/

// Tab-local by construction: each tab requests its own lock name, so two
// game tabs never wait on each other.
const LOCK_NAME_PREFIX = "sro-keep-awake-";

/*
================
createKeepAwake
================
*/
export function createKeepAwake() {
	const name = LOCK_NAME_PREFIX + crypto.randomUUID();
	let release: (() => void) | null = null;
	let disposed = false;
	/*
	================
	acquire

	The lock is held for as long as the callback's promise is pending; the
	stored resolver is the release.
	================
	*/
	function acquire() {
		const locks = typeof navigator !== "undefined" ? navigator.locks : undefined;
		if ( !locks ) return;
		const held = new Promise<void>( resolve => {
			release = resolve;
		} );
		// A refused request (a page without lock permission) leaves the tab
		// as freezable as before; it is not a session fault.
		locks.request( name, () => held ).catch( () => {} );
	}
	return {
		/*
		================
		set
		================
		*/
		set( active: boolean ) {
			if ( disposed ) return;
			if ( active && !release ) acquire();
			if ( !active && release ) {
				release();
				release = null;
			}
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			disposed = true;
			release?.();
			release = null;
		}
	};
}
