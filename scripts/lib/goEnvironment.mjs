/*
===========================================================================

goEnvironment.mjs - every checkout shares one Go build cache

Go keys a compiled package by the folder it was compiled in: the object
file's debug info names that folder, so the folder joins the cache key.
Agents work in one git worktree per task, so every tree compiled the whole
server again into the machine's one build cache, and Go drops an entry only
after five days unused. Measured 2026-11-10 on a workstation with four
agents: 39.8 GB of cache after twelve working hours, 184 copies of one
80 MB package archive among them.

With -trimpath the object file names the package's import path instead,
and the key is the import path and the contents of its files. A tree at a
commit another tree already built compiles nothing; a branch compiles only
the packages it changed and the ones that import them.

Every script that starts the Go toolchain builds its environment here.
Commands typed by hand get the same flag from the workstation
(`go env -w GOFLAGS=-trimpath`, docs/agents/windows-tooling.md). A hand
build without it still works; it only fills the cache a second time.

The price is that a compiled file no longer knows where its source is:
runtime.Caller returns "opensro.online/server/internal/..." paths, so a
test finds its files from the working directory (the package's folder
under `go test`) or config.FindModuleRoot, never from runtime.Caller.

===========================================================================
*/
import { spawnSync } from "node:child_process";

const TRIMPATH_FLAG = "-trimpath";
// Any spelling of the flag, set or cleared: the caller chose.
const TRIMPATH_ENTRY = /(^|\s)--?trimpath(=\S*)?(?=\s|$)/;
const GOFLAGS = "GOFLAGS";

/*
================
sharedCacheFlags

A GOFLAGS value with -trimpath added. The caller's own entries are kept as
they were written (Go allows quoted values, so the string is never split
and rejoined), and a caller that already set the flag either way keeps
its choice.
================
*/
export function sharedCacheFlags( current ) {
	const flags = (current ?? "").trim();
	if ( TRIMPATH_ENTRY.test( flags ) ) return flags;
	return flags.length > 0 ? `${flags} ${TRIMPATH_FLAG}` : TRIMPATH_FLAG;
}

/*
================
effectiveGoFlags

The GOFLAGS the go command would use under env: the variable, or else the
value kept by `go env -w`. Setting the variable hides the kept value, so
the flag must be added to whichever one is in force. Empty when go cannot
be started; the command that follows reports that itself.
================
*/
export function effectiveGoFlags( env = process.env ) {
	const result = spawnSync( "go", [ "env", GOFLAGS ], { windowsHide: true, env, encoding: "utf8" } );
	return result.status === 0 ? result.stdout.trim() : "";
}

/*
================
sharedCacheEnvironment

env with GOFLAGS carrying -trimpath, for every go command a script starts.
readFlags is a parameter so the decision is tested without the toolchain.
================
*/
export function sharedCacheEnvironment( env = process.env, readFlags = effectiveGoFlags ) {
	// Windows environment names are case-insensitive: replace the name the
	// caller used instead of adding a second spelling beside it.
	const key = Object.keys( env ).find( ( name ) => name.toUpperCase() === GOFLAGS ) ?? GOFLAGS;
	return { ...env, [key]: sharedCacheFlags( readFlags( env ) ) };
}
