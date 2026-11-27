/*
===========================================================================

build-metadata.mjs - identical Git stamps and source notice for every compiler

The development build (vite.config.mjs) and the beta release builder
(tools/beta/build.mjs) both read their revision, license banner and
published source-map paths from here, so a fork that rebuilds with either
keeps the same notice and ships the same readable source.

===========================================================================
*/
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

// Where the complete corresponding source lives (AGPL section 13) and the
// attribution NOTICE.md asks every version to keep (section 7(b)).
export const SOURCE_REPOSITORY = "https://github.com/opensro-dev/opensro";
// The folder a browser's devtools shows the shipped source under: the
// client's own path in the repository, so a reader finds each file there.
export const SOURCE_MAP_ROOT = "opensro/apps/client-next/";
const CLIENT_ROOT = fileURLToPath( new URL( "..", import.meta.url ) );
// The revision length the banner prints; the full id is in the bundle too.
const BANNER_REVISION_CHARS = 12;

/*
================
clientStamp

One Git read keeps revision and subject from different commits from mixing.
Source archives may not carry Git metadata; both are then empty.
================
*/
export function clientStamp( cwd = CLIENT_ROOT ) {
	try {
		const result = execFileSync( "git", [ "log", "-1", "--format=%H%n%s" ], {
			cwd,
			encoding: "utf8",
			stdio: [ "ignore", "pipe", "ignore" ]
		} ).trim().split( "\n" );
		return { revision: result[0] ?? "", subject: result.slice( 1 ).join( " " ) };
	} catch {
		return { revision: "", subject: "" };
	}
}

/*
================
clientBuildDefinitions

The compiler folds these into the artifact, never the deployment checkout.
================
*/
export function clientBuildDefinitions( cwd = CLIENT_ROOT ) {
	const { revision, subject } = clientStamp( cwd );
	return {
		"import.meta.env.SRO_CLIENT_REVISION": JSON.stringify( revision ),
		"import.meta.env.SRO_CLIENT_SUBJECT": JSON.stringify( subject )
	};
}

/*
================
licenseBanner

The legal comment every shipped chunk carries, so the notice stays in the
file even where its source map is stripped. It is a "/*!" legal comment,
which minifiers are asked to keep.
================
*/
export function licenseBanner( revision = "" ) {
	const build = revision ? ` build ${revision.slice( 0, BANNER_REVISION_CHARS )}` : "";
	return `/*! OpenSRO client${build}. AGPL-3.0-or-later with a section 7(b) attribution term: ` +
		`keep the "Powered by OpenSRO" credit and NOTICE.md. Source: ${SOURCE_REPOSITORY} */`;
}

/*
================
publishedSourcePath

The name a source map gives one module: its path under the client, behind
SOURCE_MAP_ROOT, so a map never carries a build machine's absolute path.
relative is the compiler's path from the map's own folder.
================
*/
export function publishedSourcePath( base, relative, mapPath ) {
	const absolute = path.resolve( path.dirname( mapPath ), relative );
	return SOURCE_MAP_ROOT + path.relative( base, absolute ).replaceAll( "\\", "/" );
}

/*
================
releaseSourceOutput

The output options that put the notice and the readable source into a
release: the banner on every chunk and the published source paths. The
banner goes in after minification (postBanner): a minifier dropped a
pre-minify banner from the page chunk while keeping the workers'.
================
*/
export function releaseSourceOutput( base, revision ) {
	return {
		postBanner: licenseBanner( revision ),
		sourcemapPathTransform: ( relative, mapPath ) => publishedSourcePath( base, relative, mapPath )
	};
}
