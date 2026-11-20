/*
===========================================================================

pack-mirror.ts - the desktop launcher's local copy of the asset packs

The OpenSRO desktop launcher downloads every published pack once and keeps
them on disk. On Windows it answers the page's pack requests itself
(WebView2 request interception), and the page sees nothing. macOS and Linux
webviews (WKWebView, WebKitGTK) cannot intercept an https request, so there
the launcher serves its copy under a custom scheme instead and says where by
setting PACK_MIRROR_GLOBAL before the page runs.

The asset worker then tries a pack's bytes at the mirror first and falls
back to the published origin on any failure. The mirror never serves the
manifest, so the manifest still decides which packs exist and every pack and
member is still checked against its SHA-256: a stale or wrong mirror file
costs one network read, never a wrong asset.

Outside the desktop app the global is absent and nothing here does anything.

===========================================================================
*/

// The global the desktop launcher sets in an initialization script.
export const PACK_MIRROR_GLOBAL = "__opensroPackMirror";

// The only origins a mirror may have: the launcher's scheme as each
// webview spells it (WKWebView and WebKitGTK keep the scheme; WebView2
// maps it to http(s)://<scheme>.localhost). Anything else is ignored, so
// a stray global can never point pack reads at another host.
const PACK_MIRROR_ORIGINS = [
	"sropack://localhost",
	"http://sropack.localhost",
	"https://sropack.localhost"
] as const;

/*
================
allowedOrigin
================
*/
function allowedOrigin( value: unknown ): value is string {
	return PACK_MIRROR_ORIGINS.some( origin => origin === value );
}

/*
================
publishedPack

A published pack: /assets/packs/<group>-<NNN>-<sha12>.bin. The HD set's
packs (/hd/packs/) and the manifests are never mirrored.
================
*/
function publishedPack( pathname: string ): boolean {
	return /^\/assets\/packs\/[a-z0-9_.-]+-[0-9]{3}-[0-9a-f]{12}\.bin$/.test( pathname );
}

/*
================
packMirrorOrigin

The mirror origin the desktop launcher set on this page, or null in a
browser (or for a value that is not one of the launcher's own origins).
================
*/
export function packMirrorOrigin( scope: object = globalThis ): string | null {
	const value: unknown = (scope as Record<string, unknown>)[PACK_MIRROR_GLOBAL];
	return allowedOrigin( value ) ? value : null;
}

/*
================
createPackMirror

The asset worker's view of the mirror. url() maps a published pack URL to
its mirror URL, or null when the read must go to the network: no mirror
set, not a pack, or the mirror stopped answering. missed() is told how a
mirror read failed: a status (the launcher has not got that pack yet) costs
only that read, a transport failure (the scheme does not work in this
webview) turns the mirror off for the rest of the session so later reads
do not pay for it again.
================
*/
export function createPackMirror() {
	let origin: string | null = null;
	return {
		/*
		================
		set

		The origin the page passed on; anything not allowed turns it off.
		================
		*/
		set( value: string | null ) {
			origin = allowedOrigin( value ) ? value : null;
		},
		/*
		================
		url
		================
		*/
		url( published: string ): string | null {
			if ( origin === null ) return null;
			let parsed: URL;
			try {
				parsed = new URL( published );
			} catch {
				return null;
			}
			if ( parsed.protocol !== "https:" && parsed.protocol !== "http:" ) return null;
			if ( parsed.search !== "" || !publishedPack( parsed.pathname ) ) return null;
			return origin + parsed.pathname;
		},
		/*
		================
		missed
		================
		*/
		missed( error: unknown ) {
			const answered = error instanceof Error && "status" in error && Number.isInteger( error.status );
			if ( !answered ) origin = null;
		}
	};
}
