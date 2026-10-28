/*
===========================================================================

parseJmxVisualEffect.mjs - JMXVEFF 0010..0013 binary effect reader

The tree-level layout (dataOffset u32, name, controllers, child recursion)
follows native CEFStoredEffect_DeserializeTree / CEFStoredEffect_LoadFile.
The inner member layout (globalData, programs, life/view/render commands,
resource, trailing/render programs) follows CEFStoredEffect_Deserialize
(CEFStoredEffect_ReadPayload, vtable slot +0x1c). Payload shapes for controllers and
parameters were cross-checked against the native registries in
ParticleScriptSystem_Init (CEEGlobalData_RegisterCommands) and the public JMXVEFF format
documented by JMX File Editor (Copyright 2021 Engels Quintero, MIT).

===========================================================================
*/

import { BinaryReader } from "../shared/jmxBinaryReader.mjs";

/*
================
JmxEffectReader
================
*/
class JmxEffectReader extends BinaryReader {
	/*
	================
	JmxEffectReader::constructor
	================
	*/
	constructor( buffer, sourceName = "<buffer>" ) {
		super( buffer, sourceName );
	}

	/*
	================
	JmxEffectReader::ensure
	================
	*/
	ensure( size ) {
		if ( this.offset + size > this.buffer.length ) {
			throw new Error(
				`${this.sourceName}: JMXVEFF overread at 0x${this.offset.toString( 16 )} ` +
					`(need ${size}, have ${this.buffer.length - this.offset})`
			);
		}
	}

	/*
	================
	JmxEffectReader::bool
	================
	*/
	bool() {
		return this.u8() !== 0;
	}

	/*
	================
	JmxEffectReader::fixedString
	================
	*/
	fixedString( length ) {
		return this.text( length, "JMXVEFF string" );
	}

	/*
	================
	JmxEffectReader::string
	================
	*/
	string() {
		const length = this.i32();
		if ( length < 0 || length > 8192 ) {
			throw new Error(
				`${this.sourceName}: invalid JMXVEFF string length ${length} at 0x${(this.offset - 4).toString( 16 )}`
			);
		}
		return this.fixedString( length );
	}

	/*
	================
	JmxEffectReader::vector3
	================
	*/
	vector3() {
		return [ this.f32(), this.f32(), this.f32() ];
	}

	/*
	================
	JmxEffectReader::vector4
	================
	*/
	vector4() {
		return [ this.f32(), this.f32(), this.f32(), this.f32() ];
	}

	/*
	================
	JmxEffectReader::matrix4x4
	================
	*/
	matrix4x4() {
		return Array.from( { length: 16 }, () => this.f32() );
	}

	/*
	================
	JmxEffectReader::color32
	================
	*/
	color32() {
		const b = this.u8();
		const g = this.u8();
		const r = this.u8();
		const a = this.u8();
		return [ r, g, b, a ];
	}
}

/*
================
checkedCount
================
*/
function checkedCount( reader, label, maximum = 1_000_000 ) {
	const count = reader.i32();
	if ( count < 0 || count > maximum ) {
		throw new Error(
			`${reader.sourceName}: invalid ${label} count ${count} at 0x${(reader.offset - 4).toString( 16 )}`
		);
	}
	return count;
}

/*
================
readMany
================
*/
function readMany( reader, label, read, maximum ) {
	const count = checkedCount( reader, label, maximum );
	return Array.from( { length: count }, () => read( reader ) );
}

/*
================
readStaticEmit
================
*/
function readStaticEmit( reader ) {
	return {
		min: reader.u32(),
		max: reader.u32(),
		burstRate: reader.u32(),
		minParticles: reader.u32(),
		spawnRate: reader.f32()
	};
}

/*
================
readBlend
================
*/
function readBlend( reader, readValue ) {
	return {
		begin: reader.f32(),
		end: reader.f32(),
		points: readMany(
			reader,
			"blend point",
			r => ({
				time: r.f32(),
				value: readValue( r )
			}),
			65_536
		)
	};
}

/*
================
readParameter
================
*/
function readParameter( reader, name ) {
	switch ( name ) {
		case "float":
			return { kind: name, value: reader.f32() };
		case "Vector":
			return { kind: name, value: reader.vector3() };
		case "Matrix":
			return { kind: name, value: reader.matrix4x4() };
		case "SEFStaticEmit":
			return { kind: name, value: readStaticEmit( reader ) };
		case "AxisVector4":
			return { kind: name, left: reader.vector4(), right: reader.matrix4x4() };
		case "RotVector":
			return { kind: name, left: reader.vector3(), right: reader.matrix4x4() };
		case "AngleVector1":
			return { kind: name, left: reader.vector3(), right: reader.vector3() };
		case "FrameScale":
			return { kind: name, value: readMany( reader, "FrameScale", r => r.vector3(), 65_536 ) };
		case "BlendScaleGraph":
			return { kind: name, value: readBlend( reader, r => r.vector3() ) };
		case "BlendScaleGraphPointer":
			return { kind: name, value: reader.f32() };
		case "FrameDiffuse":
			return { kind: name, value: readMany( reader, "FrameDiffuse", r => r.color32(), 65_536 ) };
		case "BlendDiffuseGraph":
			return { kind: name, value: readBlend( reader, r => r.color32() ) };
		case "FrameBANRotation":
			return {
				kind: name,
				frame: reader.f32(),
				value: readMany( reader, "FrameBANRotation", r => r.matrix4x4(), 65_536 )
			};
		case "FrameBANPosition":
			return {
				kind: name,
				frame: reader.f32(),
				value: readMany( reader, "FrameBANPosition", r => r.vector3(), 65_536 )
			};
		case "BSAnimation":
			return { kind: name, value: readMany( reader, "BSAnimation", r => r.string(), 65_536 ) };
		case "FrameTextureSlide":
			return {
				kind: name,
				frame: reader.vector3(),
				value: readMany( reader, "FrameTextureSlide", r => r.vector4(), 65_536 )
			};
		default:
			throw new Error(
				`${reader.sourceName}: unknown JMXVEFF parameter ${JSON.stringify( name )} at 0x${
					reader.offset.toString( 16 )
				}`
			);
	}
}

/*
================
readCommandParameter
================
*/
function readCommandParameter( reader, commandName ) {
	switch ( commandName ) {
		case "StaticEmit":
			return { kind: "EFStaticEmit", value: readStaticEmit( reader ) };
		case "Attraction":
			return { kind: "float", value: reader.f32() };
		case "SetPosition":
		case "SetSpherePos":
		case "SetVelocity":
		case "Force":
			return { kind: "Vector", value: reader.vector3() };
		case "SetRotationMat":
		case "SetRVelocityMat":
			return { kind: "Matrix", value: reader.matrix4x4() };
		case "SetRotation":
		case "SetRVelocity":
			return { kind: "RotVector", left: reader.vector3(), right: reader.matrix4x4() };
		case "SetRotationAxis":
		case "SetRVelocityAxis":
		case "SetShapeRot":
		case "SetShapeRotVel":
			return { kind: "AxisVector4", left: reader.vector4(), right: reader.matrix4x4() };
		case "SetConePos":
		case "SetConeVel":
		case "ConeForce":
			return { kind: "AngleVector1", left: reader.vector3(), right: reader.vector3() };
		case "SetGraphScale":
			return { kind: "FrameScale", value: readMany( reader, "SetGraphScale", r => r.vector3(), 65_536 ) };
		case "SetGraphRandomScale":
			return { kind: "BlendScaleGraphPointer", value: reader.f32() };
		case "SetGraphDiffuse":
			return { kind: "FrameDiffuse", value: readMany( reader, "SetGraphDiffuse", r => r.color32(), 65_536 ) };
		case "SetBANRot":
			return {
				kind: "FrameBANRotation",
				frame: reader.f32(),
				value: readMany( reader, "SetBANRot", r => r.matrix4x4(), 65_536 )
			};
		case "SetBANPos":
			return {
				kind: "FrameBANPosition",
				frame: reader.f32(),
				value: readMany( reader, "SetBANPos", r => r.vector3(), 65_536 )
			};
		case "TextureSlide":
			return {
				kind: "FrameTextureSlide",
				frame: reader.vector3(),
				value: readMany( reader, "TextureSlide", r => r.vector4(), 65_536 )
			};
		case "NeverExtinct":
		case "NormalTimeExtinct":
		case "NormalTimeLoop":
		case "NormalTimeLife": // Note: NormalTimeLife is a controller name (CEEGlobalData_RegisterControllerNames), not one of the 37 native commands (CEEGlobalData_RegisterCommandDescriptor); harmless compatibility fallback
		case "ViewNone":
		case "ViewBillboard":
		case "ViewVBillboard":
		case "ViewYBillboard":
		case "RenderNone":
		case "RenderMesh":
		case "RenderPlate":
		case "RenderLinkObj":
		case "RenderLinkPipe":
		case "RenderLinkDPipe":
		case "ProgramUpdate":
			return null;
		default:
			throw new Error(
				`${reader.sourceName}: unknown JMXVEFF command ${JSON.stringify( commandName )} at 0x${
					reader.offset.toString( 16 )
				}`
			);
	}
}

/*
================
readSourceNode

Deserializes one command line (native CEESourceLine_Deserialize @ 0x00b107b0).
Wire field layout vs native CEESourceListLineState struct mapping:
  reader.u8()  -> flags   (native paramKind1c   @ +0x1c)
  reader.u8()  -> byte1   (native lineFlag08    @ +0x08)
  reader.f32() -> start   (native sampleStart0c @ +0x0c, first active frame)
  reader.f32() -> spacing (native sampleStep10  @ +0x10, sample spacing / period)
  reader.f32() -> end     (native sampleEnd14   @ +0x14, schedule end frame)
================
*/
function readSourceNode( reader ) {
	if ( !reader.bool() ) return null;
	const name = reader.string();
	return {
		name,
		flags: reader.u8(),
		byte1: reader.u8(),
		start: reader.f32(),
		spacing: reader.f32(),
		end: reader.f32(),
		parameter: readCommandParameter( reader, name )
	};
}

/*
================
readProgram
================
*/
function readProgram( reader, label ) {
	return readMany( reader, label, readSourceNode, 65_536 );
}

/*
================
readResource
================
*/
function readResource( reader ) {
	const resource = {
		backFaceType: reader.u32(),
		srcBlend: reader.i32(),
		dstBlend: reader.i32(),
		srcTextureArg1: reader.i32(),
		srcTextureArg2: reader.i32(),
		srcTextureOp: reader.i32(),
		dstTextureArg1: reader.i32(),
		dstTextureArg2: reader.i32(),
		dstTextureOp: reader.i32(),
		meshes: []
	};
	resource.meshes = readMany(
		reader,
		"resource mesh",
		r => ({
			path: r.string(),
			textures: readMany( r, "mesh texture", rr => rr.string(), 65_536 )
		}),
		65_536
	);
	return resource;
}

/*
================
readController
================
*/
function readController( reader, name ) {
	switch ( name ) {
		case "NormalTimeLife":
		case "NormalTimeLoopLife":
			return { name };
		case "StaticEmit":
			return { name, value: readStaticEmit( reader ) };
		case "LinkMode":
			return { name, value: [ reader.u32(), reader.u32(), reader.u32(), reader.u32() ] };
		case "BAN":
			return { name, animations: readMany( reader, "BAN animation", r => r.string(), 65_536 ) };
		case "ViewMode": {
			const command = reader.string();
			return { name, command };
		}
		case "Shape": {
			const command = reader.string();
			return { name, command, resource: readResource( reader ) };
		}
		case "ScaleGraph":
			return {
				name,
				x: readBlend( reader, r => r.f32() ),
				y: readBlend( reader, r => r.f32() ),
				z: readBlend( reader, r => r.f32() ),
				value0: reader.f32(),
				value1: reader.f32()
			};
		case "DiffuseGraph":
			return {
				name,
				alpha: readBlend( reader, r => r.u8() ),
				diffuse: readBlend( reader, r => r.color32() )
			};
		case "Program":
			return { name, program: readProgram( reader, "controller program" ) };
		default:
			throw new Error(
				`${reader.sourceName}: unknown JMXVEFF controller ${JSON.stringify( name )} at 0x${
					reader.offset.toString( 16 )
				}`
			);
	}
}

/*
================
readGlobalData
================
*/
function readGlobalData( reader ) {
	return {
		totalFrames: reader.i32(),
		parameters: readMany(
			reader,
			"global parameter",
			r => {
				const name = r.string();
				return readParameter( r, name );
			},
			65_536
		)
	};
}

/*
================
readStoredObject
================
*/
function readStoredObject( reader, parentName = null ) {
	const recordOffset = reader.offset;
	const dataOffset = reader.u32();
	const name = reader.string();
	const controllers = readMany(
		reader,
		"controller",
		r => {
			const controllerName = r.string();
			return readController( r, controllerName );
		},
		65_536
	);
	const object = {
		name,
		parentName,
		recordOffset,
		dataOffset,
		controllers,
		globalData: readGlobalData( reader ),
		preProgram: readProgram( reader, "pre program" ),
		emitterProgram: readProgram( reader, "emitter program" ),
		postEmitterProgram: readProgram( reader, "post-emitter program" ),
		lifeCommand: readSourceNode( reader ),
		updateProgram: readProgram( reader, "update program" ),
		byte0: reader.u8(),
		byte1: reader.u8(),
		int0: reader.i32(),
		int1: reader.i32(),
		int2: reader.i32(),
		byte2: reader.u8(),
		int3: reader.i32(),
		byte3: reader.u8(),
		viewCommand: readSourceNode( reader ),
		resource: readResource( reader ),
		renderCommand: readSourceNode( reader ),
		trailingProgram: readProgram( reader, "trailing program" ),
		renderProgram: readProgram( reader, "render program" ),
		children: []
	};
	object.children = readMany( reader, "child effect", r => readStoredObject( r, name ), 65_536 );
	return object;
}

/*
================
parseJmxVisualEffect
================
*/
export function parseJmxVisualEffect( buffer, sourceName = "<buffer>" ) {
	const reader = new JmxEffectReader( buffer, sourceName );
	const signature = reader.fixedString( 8 );
	if ( signature !== "JMXVEFF " ) {
		throw new Error( `${sourceName}: invalid JMXVEFF signature ${JSON.stringify( signature )}` );
	}
	const versionText = reader.fixedString( 4 );
	const version = Number( versionText );
	if ( !Number.isInteger( version ) || version < 10 || version > 13 ) {
		throw new Error( `${sourceName}: unsupported JMXVEFF version ${JSON.stringify( versionText )}` );
	}
	// In retail v1.150 SRO_Client.exe (CEFStoredEffect_LoadFile), all 651 corpus files are version 0011.
	// The scale (v12+) and version13 header payloads are derived from Engels Quintero's
	// JMX File Editor format specification (dead code against the v1.150 corpus).
	if ( version !== 11 ) {
		console.warn(
			`${sourceName}: non-v11 JMXVEFF version ${version} encountered; verify scale/version13 layout against native binary`
		);
	}
	const scale = version >= 12 ? reader.f32() : 1;
	const version13 = version === 13 ? [ reader.i32(), reader.i32(), reader.i32() ] : null;
	const root = readStoredObject( reader );
	if ( reader.offset !== buffer.length ) {
		throw new Error(
			`${sourceName}: ${buffer.length - reader.offset} trailing JMXVEFF byte(s) at 0x${
				reader.offset.toString( 16 )
			}`
		);
	}
	return { schemaVersion: 1, version, scale, version13, root };
}

/*
================
collectJmxEffectTexturePaths
================
*/
export function collectJmxEffectTexturePaths( effect ) {
	const paths = new Set();
	const visitResource = resource => {
		for ( const mesh of resource?.meshes ?? [] ) {
			for ( const texture of mesh.textures ?? [] ) {
				if ( texture && texture.toLowerCase() !== "none" ) paths.add( texture );
			}
		}
	};
	const visit = object => {
		visitResource( object.resource );
		for ( const controller of object.controllers ?? [] ) visitResource( controller.resource );
		for ( const child of object.children ?? [] ) visit( child );
	};
	visit( effect.root );
	return [ ...paths ];
}
