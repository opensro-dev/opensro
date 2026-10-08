/*
===========================================================================

persistent-storage.ts - ask the browser to keep the verified game files

Best-effort storage is evicted whole, least recently used origin first,
when the disk runs low; persistent storage is evicted only by the player
(MDN, "Storage quotas and eviction criteria"). Chrome, Edge and Safari
grant or deny persistence silently from engagement, so the asset store's
files survive disk pressure once the player has entered the world. Firefox
answers with a permission prompt instead; its best-effort quota (the
smaller of 10% of the disk and 10 GiB) already holds the whole game, so
the client does not interrupt play to ask there.

===========================================================================
*/

type StorageNavigator = Pick<Navigator, "userAgent"> & {
	readonly storage?: Pick<StorageManager, "persist" | "persisted">;
};

/*
================
requestPersistentStorage

Resolves true when the origin's storage is (or has become) persistent.
Never throws: persistence is an optimization, not a requirement.
================
*/
export async function requestPersistentStorage( nav: StorageNavigator = navigator ): Promise<boolean> {
	if ( !nav.storage?.persist || /\bFirefox\//.test( nav.userAgent ) ) return false;
	try {
		return await nav.storage.persisted() || await nav.storage.persist();
	} catch {
		return false;
	}
}
