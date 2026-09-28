/*
===========================================================================

install.ts - background install of the combat presentation set

Retail plays every effect sound and effect on demand: the sound cache
(SoundResourceCache_Acquire, 8F7F90) loads a sound synchronously on its first
play, which costs nothing because the archive is on the local disk. In the
browser the same first play is a network round trip, so the first hit, swing
or skill of a session was late.

The play paths stay exactly as retail has them. What changes is where the
bytes are: once the world is ready, this installer walks the build's ordered
list (/assets/delivery/background-install.json: combat first, then world
sounds) and makes each file local in the persistent store. Later plays read
local bytes, as retail does.

It never competes with the game: it lives in the asset worker, uses none of
the main thread's request slots, waits whenever a foreground load is active,
fetches one file at a time, and reports no loading activity. Files already
installed by an earlier session are skipped without being read.

===========================================================================
*/

export const BACKGROUND_INSTALL_FORMAT = "sro-background-install";
export const BACKGROUND_INSTALL_VERSION = 1;
const INSTALL_LIST_BYTES = 4 << 20;
const MAX_INSTALL_PATHS = 16384;

/*
================
InstallSource
================
*/
export interface InstallSource {
	read( url: URL, limit: number, signal: AbortSignal, report?: boolean ): Promise<Uint8Array<ArrayBuffer>>;
	install( url: URL, signal: AbortSignal ): Promise<boolean>;
}

/*
================
backgroundInstallPaths

The list's paths, tier by tier in the order the build wrote them. Rejects a
list that is not the published format or names anything outside /assets/.
================
*/
export function backgroundInstallPaths( value: unknown ): string[] {
	const list = value as { format?: unknown; version?: unknown; tiers?: unknown; };
	if ( list?.format !== BACKGROUND_INSTALL_FORMAT || list.version !== BACKGROUND_INSTALL_VERSION ) {
		throw Error( "Invalid background install list" );
	}
	if ( !Array.isArray( list.tiers ) ) throw Error( "Invalid background install tiers" );
	const paths: string[] = [];
	for ( const tier of list.tiers as { name?: unknown; paths?: unknown; }[] ) {
		if ( typeof tier?.name !== "string" || !Array.isArray( tier.paths ) ) {
			throw Error( "Invalid background install tier" );
		}
		for ( const path of tier.paths ) {
			if (
				typeof path !== "string" || !path.startsWith( "/assets/" ) || path.includes( ".." ) ||
				path.includes( "\\" )
			) {
				throw Error( "Invalid background install path" );
			}
			paths.push( path );
		}
	}
	if ( paths.length > MAX_INSTALL_PATHS ) throw Error( "Background install list exceeds budget" );
	return paths;
}

/*
================
createBackgroundInstaller

`foregroundIdle` resolves when no foreground load holds capacity (the loader
wakes it as its last load settles); the installer awaits it before each
file. start() runs once per worker; later calls are ignored.
================
*/
export function createBackgroundInstaller( source: InstallSource, foregroundIdle: () => Promise<void> ) {
	const lifetime = new AbortController();
	let started = false, fetched = 0, skipped = 0, failed = 0, done = false;

	/*
================
run
================
	*/
	async function run( listUrl: URL ) {
		const bytes = await source.read( listUrl, INSTALL_LIST_BYTES, lifetime.signal, false );
		const paths = backgroundInstallPaths(
			JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes ) )
		);
		for ( const path of paths ) {
			if ( lifetime.signal.aborted ) return;
			await foregroundIdle();
			if ( lifetime.signal.aborted ) return;
			try {
				if ( await source.install( new URL( path, listUrl ), lifetime.signal ) ) fetched++;
				else skipped++;
			} catch {
				// One unavailable file does not stop the install; its first play
				// fetches it on demand, exactly as before.
				failed++;
			}
		}
		done = true;
	}

	return {
		/*
================
start
================
		*/
		start( url: string ) {
			if ( started || lifetime.signal.aborted ) return;
			started = true;
			void run( new URL( url ) ).catch( () => {
				started = false;
				failed++;
			} );
		},
		stats: () => ({ started, done, fetched, skipped, failed }),
		/*
================
dispose
================
		*/
		dispose() {
			lifetime.abort();
		}
	};
}
