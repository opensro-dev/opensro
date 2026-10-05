/*
===========================================================================

skillEffectSetParseRow.ts - one skilleffectset row, folded from sub_91e720

Owned by the asset pipeline: a v1.150 data-format table recovered from the
native client; keep behaviour identical to the published assets. This is a
DATA projection of the native row loader (CSkillEffectManager_ParseSkill-
EffectRecord @ 0x0091e720), which reads the columns from CStringProcessW and
appends a 0xa0 stage record to the effect record named by SkillEffectID.
Build tools already own a tokenized tab row, so this fold exposes the
normalized record at that archive boundary; allocation and map insertion
remain host responsibilities. The bone tokens decode here, so a published
bone name never carries the '@' / '*' markers.

===========================================================================
*/

export type SkillEffectSetVector3 = readonly [number, number, number];

export interface SkillEffectSetMove {
	kind: string;
	delay: number;
	startSpeed: number;
	endSpeed: number;
}

export interface SkillEffectSetActionOptions {
	enabled: boolean;
	direction: number;
	distance: number;
	residualDistance: number;
	lifeMs: number;
	simultaneousRelease: boolean;
}

export interface SkillEffectSetRecord {
	displayName: string;
	skillEffectId: string;
	animationPhase: "READY" | "WAIT" | "SHOT" | "ACT_S" | string;
	startEvent: number;
	damageEvent: boolean;
	damageTypes: readonly string[];
	scale: string | number | null;
	id: number;
	/** Native stage+0x06 attachment-slot selector; zero means no slot. */
	attach: number;
	/** Native stage+0x07 staged-command transfer key; zero means no transfer. */
	trade: number;
	/** Native stage+0x08 release-slot selector; zero means no release. */
	kill: number;
	createCount: number;
	fadeInMs: number;
	fadeOutMs: number;
	actionType: string;
	move: SkillEffectSetMove;
	param: SkillEffectSetVector3;
	actionOptions: SkillEffectSetActionOptions;
	objectDirectory: string | null;
	objectName: string | null;
	objectResourcePath: string | null;
	/** Bone name with the token's '@' / '*' marker removed; null at the root. */
	startBone: string | null;
	/** Native attach +0x08: the bone's rotation is kept (8D6880 resets it to
	 * identity when this is false, keeping only the bone position). */
	startKeepRotation: boolean;
	/** Native attach +0x09: the '*' token, world Y += character height. */
	startAddHeight: boolean;
	startOffset: SkillEffectSetVector3;
	targetBone: string | null;
	targetKeepRotation: boolean;
	targetAddHeight: boolean;
	targetOffset: SkillEffectSetVector3;
	secondaryObjectPath: string | null;
	rotate: number;
	scripts: readonly string[];
	soundBegin: string | null;
	soundEnd: string | null;
	/** Browser archive resolution of native CPrimSound descriptors. These are
	 * host annotations, absent from the 0xa0 native row itself. */
	soundBeginPublicPath?: string | null;
	soundEndPublicPath?: string | null;
	/** Build-boundary provenance when newer server textdata requires a
	 * data-proved discriminator translation into this v1.150 client schema. */
	clientCompatibility?: {
		field: "move.kind";
		sourceValue: string;
		clientValue: string;
		evidenceSkillEffectIds: readonly string[];
	};
}

/*
================
noneToNull
================
*/
function noneToNull( value: unknown ): string | null {
	const text = String( value ?? "" ).trim();
	return !text || text.toLowerCase() === "none" ? null : text;
}

/* One decoded 0x18-byte attachment binding (bone, +0x08, +0x09). */
interface SkillEffectSetBinding {
	bone: string | null;
	keepRotation: boolean;
	addHeight: boolean;
}

/*
================
bindingAt

The bone token arms of sub_91e720 (0x91f3b2 start, 0x91f520 target) over a
record that 0x91eaca memsets to zero: "none" leaves the zero binding, "*"
keeps rotation and adds the character height with no bone, "@Name" binds
Name without its rotation, and a plain name binds it with its rotation.
================
*/
function bindingAt( value: unknown ): SkillEffectSetBinding {
	const text = noneToNull( value );
	if ( text === null ) return { bone: null, keepRotation: false, addHeight: false };
	if ( text.startsWith( "*" ) ) return { bone: null, keepRotation: true, addHeight: true };
	if ( text.startsWith( "@" ) ) return { bone: text.slice( 1 ), keepRotation: false, addHeight: false };
	return { bone: text, keepRotation: true, addHeight: false };
}

/*
================
numberAt
================
*/
function numberAt( value: unknown, fallback = 0 ): number {
	const parsed = Number( String( value ?? "" ).trim() );
	return Number.isFinite( parsed ) ? parsed : fallback;
}

/*
================
boolAt
================
*/
function boolAt( value: unknown ): boolean {
	const text = String( value ?? "" ).trim().toUpperCase();
	return text === "TRUE" || text === "1";
}

/*
================
tuple
================
*/
function tuple( value: unknown, count: number ): number[] {
	const fields = String( value ?? "" ).split( "," );
	return Array.from( { length: count }, ( _, index ) => numberAt( fields[index] ) );
}

/*
================
vector3
================
*/
function vector3( value: unknown ): SkillEffectSetVector3 {
	const values = tuple( value, 3 );
	return [ values[0]!, values[1]!, values[2]! ];
}

/*
================
resourcePath
================
*/
function resourcePath( directory: string | null, name: string | null ): string | null {
	if ( !name ) return null;
	const joined = `${directory ?? ""}${name}`
		.replaceAll( "\\", "/" )
		.replace( /^\/+/, "" )
		.toLowerCase();
	return joined || null;
}

/*
================
SkillEffectSet_ParseRow

Fold of sub_91e720's field-reader body.  The caller supplies either the
original 28-column tab row or those columns already split.  Delimiter
sub-records preserve their native arity: fade(2), move(4), param(3),
action options(6), and both offsets(3).
================
*/
export function SkillEffectSet_ParseRow(
	row: string | readonly string[]
): SkillEffectSetRecord {
	const c = typeof row === "string" ? row.split( "\t" ) : row;
	if ( c.length < 28 ) {
		throw new Error( `SkillEffectSet_ParseRow: expected 28 columns, got ${c.length}` );
	}

	const fade = tuple( c[12], 2 );
	const move = String( c[14] ?? "" ).split( "," );
	const options = String( c[16] ?? "" ).split( "," );
	const objectDirectory = noneToNull( c[17] );
	const objectName = noneToNull( c[18] );
	const scaleText = noneToNull( c[6] );
	let start = bindingAt( c[19] );
	const target = bindingAt( c[21] );
	// 0x91f526..0x91f535: the target '*' arm stores its bone and +0x08 into
	// the START binding (esp+0x90 / esp+0x98) and only +0x09 into the target.
	// Every v1.150 row with a target '*' has start "none", so this decides only
	// that start's rotation flag; it is kept as the original wrote it.
	const targetStar = target.addHeight;
	if ( targetStar ) start = { ...start, bone: null, keepRotation: true };
	const numericScale = scaleText === null ? Number.NaN : Number( scaleText );

	return {
		displayName: String( c[0] ?? "" ),
		skillEffectId: String( c[1] ?? "" ).trim(),
		animationPhase: String( c[2] ?? "" ).trim().toUpperCase(),
		startEvent: numberAt( c[3] ),
		damageEvent: boolAt( c[4] ),
		damageTypes: String( c[5] ?? "" )
			.split( "|" )
			.map( ( value ) => value.trim().toUpperCase() )
			.filter( ( value ) => value && value !== "NONE" ),
		scale: scaleText === null ? null : Number.isFinite( numericScale ) ? numericScale : scaleText,
		id: numberAt( c[7] ),
		// These three columns look boolean in many early rows, but they are byte
		// slot/index values.  0x91ebcd..0x91ec08 calls the integer reader four
		// times for ID/Attach/Trade/Kill and stores AL into four consecutive
		// record bytes.  The shipped corpus uses Attach=2, Trade=2, Kill=2..6.
		attach: numberAt( c[8] ),
		trade: numberAt( c[9] ),
		kill: numberAt( c[10] ),
		createCount: numberAt( c[11], 1 ),
		fadeInMs: fade[0]!,
		fadeOutMs: fade[1]!,
		actionType: String( c[13] ?? "" ).trim().toUpperCase(),
		move: {
			kind: String( move[0] ?? "MOV_NONE" ).trim().toUpperCase(),
			delay: numberAt( move[1] ),
			startSpeed: numberAt( move[2] ),
			endSpeed: numberAt( move[3] )
		},
		param: vector3( c[15] ),
		actionOptions: {
			enabled: boolAt( options[0] ),
			direction: numberAt( options[1] ),
			distance: numberAt( options[2] ),
			residualDistance: numberAt( options[3] ),
			lifeMs: numberAt( options[4] ),
			simultaneousRelease: boolAt( options[5] )
		},
		objectDirectory,
		objectName,
		objectResourcePath: resourcePath( objectDirectory, objectName ),
		startBone: start.bone,
		startKeepRotation: start.keepRotation,
		startAddHeight: start.addHeight,
		startOffset: vector3( c[20] ),
		targetBone: target.bone,
		targetKeepRotation: targetStar ? false : target.keepRotation,
		targetAddHeight: target.addHeight,
		targetOffset: vector3( c[22] ),
		secondaryObjectPath: resourcePath( null, noneToNull( c[23] ) ),
		rotate: numberAt( c[24] ),
		scripts: String( c[25] ?? "" )
			.split( "," )
			.map( ( value ) => value.trim() )
			.filter( ( value ) => value && value.toLowerCase() !== "none" ),
		soundBegin: resourcePath( null, noneToNull( c[26] ) ),
		soundEnd: resourcePath( null, noneToNull( c[27] ) )
	};
}
