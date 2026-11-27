/*
===========================================================================

ui-action-census-refresh.mjs - the daily census refresh at main

#4337 item 21: a stale committed census
(tests/runtime/ui-action-census.json) is otherwise found at release time,
because CI has no game data and cannot run the census at all, while the
local gate (ui-action-census-gate.mjs) only runs where the data is. This
runner is the daily bridge, meant for the operator's scheduled task on
the data-holding PC:

  node apps/client-next/tools/ui-action-census-refresh.mjs [--dry-run]

Once a day it checks the census at origin/main in a throwaway worktree
(never the hub checkout): the gate runs there, and only a failure pays
for a real `ui-action-census.mjs --write`. When the write moves the
committed census and no refresh pull request is already open, it pushes
a dated branch holding only the census file and opens the refresh PR.
The release then never finds a stale census first, and no new gate is
added to anyone's check.

Safety shape:
  - the hub checkout is only read (fetch, worktree add/remove);
  - the census file is the only file ever committed;
  - a write that fails, or changes nothing after a gate failure, opens
    no PR and exits 1 - a broken tool never becomes a refresh PR;
  - an already-open refresh PR suppresses a new one; the next day runs
    again after it lands.

Operator wiring (opensro3, mirroring the release train's Daily task in
workflow/ops/register_tasks.ps1): one Daily scheduled task running the
command above from the hub checkout. `--dry-run` rehearses the whole
pass and reports the decision without branching, pushing or opening.

===========================================================================
*/

import { spawn, spawnSync } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const REFRESH_BRANCH_PREFIX = "chore/ui-action-census-refresh";
export const CENSUS_REPO_PATH = "apps/client-next/tests/runtime/ui-action-census.json";

const TOOLS_DIR = path.dirname( fileURLToPath( import.meta.url ) );
const REPO_ROOT = path.resolve( TOOLS_DIR, "../../.." );

/*
================
parseRefreshArgs

The runner's one switch: --dry-run reports the decision and skips the
branch, push and pull request. Anything else is a usage error.
================
*/
export function parseRefreshArgs( args ) {
	const parsed = { dryRun: false };
	for ( const argument of args ) {
		if ( argument === "--dry-run" ) {
			parsed.dryRun = true;
		} else {
			throw new Error( `unknown census refresh option ${argument} (only --dry-run)` );
		}
	}
	return parsed;
}

/*
================
censusRefreshDecision

What one daily pass does, from its four facts: the gate's verdict, an
already-open refresh PR, and (only after a gate failure) the write's
exit code and whether it moved the committed census.

  current     the gate passed: nothing to do.
  suppressed  a refresh PR is already open: it carries the refresh;
              tomorrow's pass re-checks after it lands.
  refresh     the write moved the census: branch and open the PR.
  failed      the census tool is broken (failed without moving the
              census, or failed while moving it): never a PR.
================
*/
export function censusRefreshDecision( facts ) {
	if ( facts.gatePassed ) return "current";
	if ( facts.openRefreshPr ) return "suppressed";
	if ( facts.writeExitCode !== 0 ) return "failed";
	return facts.writeChanged ? "refresh" : "failed";
}

/*
================
refreshBranchName

One branch per day, UTC: a landed or closed day's branch is never
reused, so a push never meets a stale remote tip.
================
*/
export function refreshBranchName( now = new Date() ) {
	return `${REFRESH_BRANCH_PREFIX}-${now.toISOString().slice( 0, 10 ).replaceAll( "-", "" )}`;
}

/*
================
refreshPrTitle
================
*/
export function refreshPrTitle() {
	return "chore(client): refresh the UI action census (#4337 item 21)";
}

/*
================
refreshPrBody

The refresh PR's text. It names the item it serves and the exact diff
the census write produced, so the review is reading the row diff, not
re-running the tool.
================
*/
export function refreshPrBody( facts ) {
	return [
		"Refs #4337 (item 21: a stale committed UI action census is only found at release time)",
		"",
		"The daily census refresh (`apps/client-next/tools/ui-action-census-refresh.mjs`) ran the",
		"census gate at origin/main on the data-holding PC: the gate failed, and",
		"`node tools/ui-action-census.mjs --write` moved the committed census. This PR is",
		"that write and nothing else, so the release never finds the stale census first.",
		"",
		`\`git diff --numstat\` for the census file (added/deleted lines):`,
		"",
		"```",
		facts.numstat.trim(),
		"```",
		"",
		"Review the row diff; the census is generated, never edited by hand."
	].join( "\n" );
}

/*
================
git

One git call, output captured; a failure throws with git's own text.
================
*/
function git( args, cwd ) {
	const out = spawnSync( "git", args, { cwd, encoding: "utf8", windowsHide: true } );
	if ( out.status !== 0 ) {
		throw new Error( `git ${args[0]} failed: ${(out.stderr || out.stdout || "").trim().slice( 0, 400 )}` );
	}
	return (out.stdout || "").trim();
}

/*
================
ghJson

One gh call whose stdout is JSON (the open-refresh check).
================
*/
function ghJson( args, cwd ) {
	const out = spawnSync( "gh", args, { cwd, encoding: "utf8", windowsHide: true } );
	if ( out.status !== 0 ) {
		throw new Error( `gh ${args.slice( 0, 2 ).join( " " )} failed: ${(out.stderr || "").trim().slice( 0, 400 )}` );
	}
	return JSON.parse( out.stdout || "[]" );
}

/*
================
runNodeTool

One of the census tools in the scratch tree's client, output inherited:
the gate on a skip is one line; a real census run is the day's evidence.
================
*/
function runNodeTool( clientDir, tool, args = [] ) {
	return new Promise( ( resolvePromise ) => {
		const child = spawn( process.execPath, [ path.join( clientDir, "tools", tool ), ...args ], {
			cwd: clientDir,
			stdio: "inherit",
			windowsHide: true
		} );
		child.on( "error", () => resolvePromise( 1 ) );
		child.on( "close", code => resolvePromise( code ?? 1 ) );
	} );
}

/*
================
installScratch

The census's source model parses TypeScript with the typescript
package, so the scratch tree needs the workspace install before the
gate can run - the worktree rule (frozen, offline, seconds from the
shared store). The arguments are constants; the shell only resolves
pnpm's Windows shim.
================
*/
function installScratch( scratch ) {
	const out = spawnSync( "pnpm install --frozen-lockfile --offline", {
		cwd: scratch,
		shell: true,
		stdio: "inherit",
		windowsHide: true
	} );
	if ( out.status !== 0 ) {
		throw new Error( `pnpm install in the census scratch tree failed (${out.status})` );
	}
}

/*
================
openRefreshPrExists

An open PR whose head is any day's refresh branch: its review carries
the refresh, so a new one would only split it.
================
*/
function openRefreshPrExists( cwd ) {
	const prs = ghJson( [ "pr", "list", "--state", "open", "--json", "headRefName", "--limit", "100" ], cwd );
	return prs.some( pr => pr.headRefName && pr.headRefName.startsWith( REFRESH_BRANCH_PREFIX ) );
}

/*
================
publishRefresh

Branch the scratch tree, commit only the census file, push and open
the refresh PR. The commit is the census write, byte for byte.
================
*/
function publishRefresh( scratch, branch ) {
	const numstat = git( [ "diff", "--numstat", "--", CENSUS_REPO_PATH ], scratch );
	git( [ "checkout", "-b", branch ], scratch );
	git( [ "add", "--", CENSUS_REPO_PATH ], scratch );
	git( [ "commit", "-m", refreshPrTitle(), "--", CENSUS_REPO_PATH ], scratch );
	git( [ "push", "-u", "origin", branch ], scratch );
	const out = spawnSync(
		"gh",
		[
			"pr",
			"create",
			"--title",
			refreshPrTitle(),
			"--body",
			refreshPrBody( { numstat } ),
			"--head",
			branch,
			"--base",
			"main"
		],
		{ cwd: scratch, encoding: "utf8", windowsHide: true }
	);
	if ( out.status !== 0 ) {
		throw new Error( `gh pr create failed: ${(out.stderr || "").trim().slice( 0, 400 )}` );
	}
	return (out.stdout || "").trim();
}

/*
================
main

Fetch main, scratch worktree, gate, maybe write, decide, maybe publish.
The scratch tree is removed whatever happens; the hub is left as found.
================
*/
async function main() {
	const options = parseRefreshArgs( process.argv.slice( 2 ) );
	git( [ "fetch", "-q", "origin", "main" ], REPO_ROOT );
	git( [ "worktree", "prune" ], REPO_ROOT );
	const scratch = await mkdtemp( path.join( os.tmpdir(), "sro-census-refresh-" ) );
	try {
		git( [ "worktree", "add", "-q", "--detach", scratch, "origin/main" ], REPO_ROOT );
		installScratch( scratch );
		const clientDir = path.join( scratch, "apps/client-next" );
		const gateExit = await runNodeTool( clientDir, "ui-action-census-gate.mjs" );
		const facts = { gatePassed: gateExit === 0, openRefreshPr: false, writeExitCode: 0, writeChanged: false };
		if ( !facts.gatePassed ) {
			facts.openRefreshPr = openRefreshPrExists( scratch );
			if ( !facts.openRefreshPr ) {
				facts.writeExitCode = await runNodeTool( clientDir, "ui-action-census.mjs", [ "--write" ] );
				facts.writeChanged = git( [ "status", "--porcelain", "--", CENSUS_REPO_PATH ], scratch ) !== "";
			}
		}
		const decision = censusRefreshDecision( facts );
		if ( decision === "current" ) {
			console.log( "census refresh: main's census is current; nothing to do." );
			return;
		}
		if ( decision === "suppressed" ) {
			console.log(
				"census refresh: main's census is stale, but a refresh PR is already open; leaving it to land."
			);
			return;
		}
		if ( decision === "failed" ) {
			console.error(
				`census refresh: the census write exited ${facts.writeExitCode} ` +
					`and moved the census: ${facts.writeChanged}. No PR opened; the tool needs a look.`
			);
			process.exitCode = 1;
			return;
		}
		const branch = refreshBranchName();
		if ( options.dryRun ) {
			console.log(
				`census refresh: DRY RUN - would push ${branch} with the census write and open the refresh PR.`
			);
			return;
		}
		const url = publishRefresh( scratch, branch );
		console.log( `census refresh: opened ${url} (${branch}).` );
	} finally {
		// Best effort both ways: a handle a crashed census child still
		// holds must not mask the pass's own verdict, and the next run's
		// `worktree prune` clears a registration whose tree is gone.
		try {
			git( [ "worktree", "remove", "--force", scratch ], REPO_ROOT );
		} catch {
			// The tree stays in the temp dir for the OS to reap.
		}
		await rm( scratch, { recursive: true, force: true } ).catch( () => {} );
	}
}

if ( process.argv[1] === fileURLToPath( import.meta.url ) ) {
	main().catch( error => {
		console.error( error );
		process.exitCode = 1;
	} );
}
