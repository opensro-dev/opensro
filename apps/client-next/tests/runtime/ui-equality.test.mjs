/*
===========================================================================

ui-equality.test.mjs - tests for ui-equality.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { sameUiQuads, sameUiSemantics } = await import(
	sourceFileUrl( "src/engine/foundation/ui/ui-equality.ts" ).href
);
const quad = () => ({
	texture: "atlas",
	rect: [ 1, 2, 3, 4 ],
	clip: [ 0, 0, 100, 100 ],
	uv: [ 0, 0, 1, 1 ],
	color: [ 1, 1, 1, 1 ]
});
test("retained UI compares pixels, anchors, masks and preview state without losing updates", () => {
	const first = quad();
	assert.equal( sameUiQuads( [ first ], [ structuredClone( first ) ] ), true );
	for ( const key of [ "rect", "clip", "uv", "color" ] ) {
		for ( let i = 0; i < 4; i++ ) {
			const changed = structuredClone( first );
			changed[key][i] += .1;
			assert.equal( sameUiQuads( [ first ], [ changed ] ), false );
		}
	}
	for (
		const extra of [
			{ occlusion: "none" },
			{ occlusion: "scene" },
			{ rightColor: [ 0, 0, 0, 0 ] },
			{ texture: "new" },
			{ layer: "background" },
			{ depth: .5 },
			{ rotation: .1 },
			{ uvTurn: 1 },
			{ alphaCutoff: .5 },
			{ tiled: true },
			{ additive: true },
			{ characterAnchor: 5 },
			{ portraitGid: 5 },
			{ mask: { texture: "mask", rect: [ 0, 0, 1, 1 ] } },
			{ doll: { gid: 5, yaw: 1 } },
			{ worldAnchor: { regionId: 1, x: 2, y: 3, z: 4 } }
		]
	) {
		const changed = { ...first, ...extra };
		assert.equal( sameUiQuads( [ first ], [ changed ] ), false );
		assert.equal( sameUiQuads( [ changed ], [ structuredClone( changed ) ] ), true );
	}
	assert.equal( sameUiQuads( [ first ], [] ), false );
});
/** @type {Required<Extract<import("../../src/engine/contracts/ui").UiHelpSource, { kind: "effect" }>>} */
const effectHelp = { kind: "effect", gid: 7, token: 3, skill: 11, viewer: true, suppressed: true };
/** @type {Required<Extract<import("../../src/engine/contracts/ui").UiHelpSource, { kind: "abnormal" }>>} */
const abnormalHelp = { kind: "abnormal", gid: 7, bit: 4, viewer: true, unlevelled: true };
/*
================
everyControlField

A control with every field of the UiControl contract set. Required<> makes
verify:test-types fail when the contract gains a field this sample lacks,
so a new field cannot skip the comparison below unnoticed.
================
*/
function everyControlField( id, helpSource ) {
	/** @type {Required<import("../../src/engine/contracts/ui").UiControl>} */
	const control = {
		hitPolygon: [ [ 0, 0 ], [ 10, 0 ], [ 5, 8 ] ],
		multiline: true,
		valueText: "5 of 9",
		whisperTarget: "Friend",
		textAlign: "left",
		textInsets: [ 1, 2, 3, 4 ],
		rightActivate: true,
		helpSource,
		helpText: "Help",
		captureKeys: true,
		draggable: true,
		carry: true,
		inspect: true,
		keepFocusOnAltPress: true,
		min: 1,
		max: 9,
		maxLength: 20,
		id,
		label: "Label",
		rect: [ 10, 20, 30, 40 ],
		kind: "text",
		value: "value",
		disabled: true,
		selected: true
	};
	return control;
}
/** @type {Required<import("../../src/engine/contracts/ui").UiSemantics>} */
const everySemanticsField = {
	loadingVisible: true,
	titleCredit: true,
	hudCorner: [ 1, 2, 3, 4 ],
	hudToolsTop: 80,
	/** @type {Required<NonNullable<import("../../src/engine/contracts/ui").UiSemantics["focusRequest"]>>} */
	focusRequest: { id: "chat", revision: 1, caret: 2, anchor: 1 },
	loadingStatus: "Loading",
	loadingProgress: .5,
	loading: true,
	loadingError: "failed",
	title: "world",
	message: "ready",
	controls: [ everyControlField( "chat", effectHelp ), everyControlField( "buff", abnormalHelp ) ]
};
/*
================
leafChanges

Every copy of value that differs from it in exactly one leaf (a number,
string or boolean at any depth), plus each array one element shorter.
Each comes with the path of what changed.
================
*/
function* leafChanges( value, path = "" ) {
	if ( Array.isArray( value ) ) {
		for ( let i = 0; i < value.length; i++ ) {
			for ( const [at, changed] of leafChanges( value[i], `${path}[${i}]` ) ) {
				yield [ at, [ ...value.slice( 0, i ), changed, ...value.slice( i + 1 ) ] ];
			}
		}
		yield [ `${path}.length`, value.slice( 0, -1 ) ];
	} else if ( value && typeof value === "object" ) {
		for ( const key of Object.keys( value ) ) {
			for ( const [at, changed] of leafChanges( value[key], `${path}.${key}` ) ) {
				yield [ at, { ...value, [key]: changed } ];
			}
		}
	} else yield [ path, typeof value === "number" ? value + 1 : typeof value === "string" ? value + "!" : !value ];
}
test("every field of the semantics contract, at every depth, makes semantics unequal", () => {
	assert.equal( sameUiSemantics( everySemanticsField, structuredClone( everySemanticsField ) ), true );
	for ( const [path, changed] of leafChanges( everySemanticsField ) ) {
		assert.equal( sameUiSemantics( everySemanticsField, changed ), false, path );
		assert.equal( sameUiSemantics( changed, everySemanticsField ), false, path );
	}
	for ( const absent of [ "focusRequest", "hudCorner" ] ) {
		assert.equal( sameUiSemantics( everySemanticsField, { ...everySemanticsField, [absent]: undefined } ), false );
	}
	const [chat] = everySemanticsField.controls;
	for ( const absent of [ "helpSource", "hitPolygon", "textInsets" ] ) {
		const changed = {
			...everySemanticsField,
			controls: [ { ...chat, [absent]: undefined }, everySemanticsField.controls[1] ]
		};
		assert.equal( sameUiSemantics( everySemanticsField, changed ), false, absent );
	}
});

test("a doll quad whose camera moved is a new quad (the 3D viewer's zoom)", () => {
	const camera = { eye: [ 0, 1, -5 ], target: [ 0, 1, 0 ], fov: 0.6, near: 1, far: 10 };
	const doll = { ...quad(), doll: { gid: 1, yaw: 0, camera } };
	assert.equal( sameUiQuads( [ doll ], [ structuredClone( doll ) ] ), true );
	const closer = structuredClone( doll );
	closer.doll.camera.eye[2] = -4;
	assert.equal( sameUiQuads( [ doll ], [ closer ] ), false );
	const plain = { ...quad(), doll: { gid: 1, yaw: 0 } };
	assert.equal( sameUiQuads( [ doll ], [ plain ] ), false );
});

test("a doll quad that gains the viewer's ride is a new quad", () => {
	const plain = { ...quad(), doll: { gid: 1, yaw: 0 } };
	assert.equal( sameUiQuads( [ plain ], [ { ...plain, doll: { gid: 1, yaw: 0, mount: true } } ] ), false );
});
