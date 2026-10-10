/*
===========================================================================

packPublicTree.test.mjs - the ordered pack tail, run with recording steps

Both full pack builds end in packPublicTree. These tests run that exact
sequence with stub steps: sidecars refresh before packing and again after
the web manifest, the collector's groups reach the pack builder, missing
pack files fail the build, and only a full build retires sidecars.

===========================================================================
*/
import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { missingPackFiles, packPublicTree } from "../../build/packPublicTree.mjs";
import { REQUIRED_RUNTIME_TEXT_ASSETS } from "../../build/assetPackOwnership.mjs";

const PUBLIC_ROOT = path.resolve( "fixture-public" );
const GROUPS = [ { name: "game-data", files: [ "/assets/data/a.json.gz", ...REQUIRED_RUNTIME_TEXT_ASSETS ] } ];
const PACKS = {
	groups: [ { name: "game-data", packs: [ { path: "/assets/packs/game-data-001-abcdefabcdef.bin" } ] } ]
};

/*
================
recordingSteps

Stub steps that record each call in order. present decides which pack files
exist.
================
*/
function recordingSteps( present = () => true ) {
	const calls = [];
	const steps = {
		optimizeJson: async () => {
			calls.push( [ "optimizeJson" ] );
			return { pass: calls.length };
		},
		collectGroups: async ( inputs ) => {
			calls.push( [ "collectGroups", inputs ] );
			return { groups: GROUPS };
		},
		buildPacks: async ( request ) => {
			calls.push( [ "buildPacks", request ] );
			return PACKS;
		},
		packFileExists: present,
		retireSidecars: async ( request ) => {
			calls.push( [ "retireSidecars", request ] );
			return { retired: [] };
		},
		buildWebManifest: async () => {
			calls.push( [ "buildWebManifest" ] );
			return { files: [] };
		}
	};
	return { calls, steps };
}

/*
================
TestFullBuildTailOrder
================
*/
test("a full build refreshes, collects, packs, retires, registers, then refreshes again", async () => {
	const { calls, steps } = recordingSteps();
	const inputs = { uiImagePreloadPaths: [ "/a.png" ], missionMinimapTilePaths: [], includeOutdoorWorld: true };
	const result = await packPublicTree(
		{ publicRoot: PUBLIC_ROOT, retireSidecars: true, groupInputs: inputs },
		steps
	);
	assert.deepEqual( calls.map( ( call ) => call[0] ), [
		"optimizeJson",
		"collectGroups",
		"buildPacks",
		"retireSidecars",
		"buildWebManifest",
		"optimizeJson"
	] );
	assert.equal( calls[1][1], inputs, "the caller's group inputs reach the shared collector" );
	assert.equal( calls[2][1].groups, GROUPS, "the pack builder receives the collector's groups" );
	assert.deepEqual( calls[3][1], { publicRoot: PUBLIC_ROOT, apply: true } );
	assert.equal( result.assetPacks, PACKS );
});

/*
================
TestRepackNeverRetires
================
*/
test("a repack never retires sidecars (a partial build proves no absence)", async () => {
	const { calls, steps } = recordingSteps();
	const result = await packPublicTree( { publicRoot: PUBLIC_ROOT, retireSidecars: false, groupInputs: {} }, steps );
	assert.ok( !calls.some( ( call ) => call[0] === "retireSidecars" ) );
	assert.equal( result.sidecarRetirement, null );
});

/*
================
TestMissingPackFails
================
*/
test("a pack the index names but the tree lacks fails before the web manifest", async () => {
	const { calls, steps } = recordingSteps( () => false );
	await assert.rejects(
		packPublicTree( { publicRoot: PUBLIC_ROOT, retireSidecars: true, groupInputs: {} }, steps ),
		/missing pack file\(s\): \/assets\/packs\/game-data-001-abcdefabcdef\.bin/
	);
	assert.ok( !calls.some( ( call ) => call[0] === "buildWebManifest" ) );
});

/*
================
TestPackPresence
================
*/
test("only the pack file itself counts as present", () => {
	const pack = path.join( PUBLIC_ROOT, "assets/packs/game-data-001-abcdefabcdef.bin" );
	assert.deepEqual( missingPackFiles( PACKS, PUBLIC_ROOT, ( file ) => file === pack ), [] );
	assert.deepEqual( missingPackFiles( PACKS, PUBLIC_ROOT, ( file ) => file === pack + ".zst" ), [
		"/assets/packs/game-data-001-abcdefabcdef.bin"
	] );
});

/*
================
TestClaimAuditFeedsThePacker
================
*/
test("the claim audit runs after collection and the packer receives the audited groups", async () => {
	const { calls, steps } = recordingSteps();
	const audited = [ { name: "game-data", files: [ ...REQUIRED_RUNTIME_TEXT_ASSETS ] } ];
	steps.auditClaims = async ( groups ) => {
		calls.push( [ "auditClaims", groups ] );
		return { archived: true, groups: audited };
	};
	const result = await packPublicTree(
		{ publicRoot: PUBLIC_ROOT, retireSidecars: true, auditClaims: true, groupInputs: {} },
		steps
	);
	assert.deepEqual( calls.slice( 1, 4 ).map( ( call ) => call[0] ), [
		"collectGroups",
		"auditClaims",
		"buildPacks"
	] );
	assert.equal( calls[2][1], GROUPS, "the audit sees every collected group" );
	assert.equal( calls[3][1].groups, audited, "unclaimed files never reach the packer" );
	assert.equal( result.claimAudit.archived, true );
});

/*
================
TestMissingRuntimeTableStopsPublication
================
*/
test("a table omitted by collection or claim audit stops before pack writes", async () => {
	for ( const missing of REQUIRED_RUNTIME_TEXT_ASSETS ) {
		for ( const auditClaims of [ false, true ] ) {
			const { calls, steps } = recordingSteps();
			const incomplete = [ { name: "game-data", files: GROUPS[0].files.filter( name => name !== missing ) } ];
			if ( auditClaims ) steps.auditClaims = async () => ({ groups: incomplete });
			else steps.collectGroups = async () => ({ groups: incomplete });
			await assert.rejects(
				packPublicTree( {
					publicRoot: PUBLIC_ROOT,
					groupInputs: {},
					auditClaims
				}, steps ),
				error => error.message.includes( missing )
			);
			assert.ok( !calls.some( call => call[0] === "buildPacks" || call[0] === "buildWebManifest" ) );
		}
	}
});
