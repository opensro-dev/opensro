/*
===========================================================================

texture-stage.test.mjs - native stage-0 ops for effect resources

CEFEffect_Render (B153A0) sets COLOROP/ALPHAOP from the resource. The alpha
op decides how strongly an additive effect shows: MODULATE2X doubles it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { stageResult, packTextureStage, validTextureStage } = await import(
	"../../src/engine/foundation/rendering/texture-stage.ts"
);

test("modulate ops scale texture by diffuse and saturate", () => {
	assert.equal( stageResult( 4, 2, 0, 0.5, 0.5 ), 0.25 );
	assert.equal( stageResult( 5, 2, 0, 0.5, 0.5 ), 0.5 );
	assert.equal( stageResult( 6, 2, 0, 0.5, 0.5 ), 1 );
	assert.equal( stageResult( 6, 2, 0, 0.75, 0.75 ), 1 );
});

test("select ops ignore the other argument", () => {
	// SELECTARG1 TEXTURE: a diffuse fade cannot dim the texture alpha.
	assert.equal( stageResult( 2, 2, 0, 0.8, 0.1 ), 0.8 );
	assert.equal( stageResult( 3, 2, 0, 0.8, 0.1 ), 0.1 );
});

test("packing keeps both stages and marks native mode", () => {
	const fire = { colorOp: 5, colorArg1: 2, colorArg2: 0, alphaOp: 4, alphaArg1: 2, alphaArg2: 0 };
	assert.deepEqual( packTextureStage( fire ), [ 5, 32, 4, 32 ] );
	assert.deepEqual( packTextureStage( undefined ), [ 0, 0, 0, 0 ] );
	assert.equal( validTextureStage( fire ), true );
	assert.equal( validTextureStage( { ...fire, colorOp: 7 } ), false );
	// DISABLE is authored only for alpha: the diffuse alpha passes through.
	assert.equal( validTextureStage( { ...fire, alphaOp: 1 } ), true );
	assert.equal( validTextureStage( { ...fire, colorOp: 1 } ), false );
	assert.equal( stageResult( 1, 2, 0, 0.2, 0.7 ), 0.7 );
	assert.equal( validTextureStage( { ...fire, alphaArg1: 3 } ), false );
});
