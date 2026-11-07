/*
===========================================================================

crest-register.ts - the crest register items and their status lines

#2282 (register half): ITEM_ETC_GUILD_CREST_USER (9268) and
ITEM_ETC_UNION_CREST_USER (9270) share the type word 3/3/12/2 with
their delete twins, so the shipped reference id alone tells a register
from a delete (crest-delete.ts). The native trigger (item-use click)
opens the confirm box 0x15 (guild) / 0x18 (union); its OK arm refuses
with UIIT_MSG_CREST_REGISTER_OVERLAP when a crest already stands, then
validates the local bitmap (1 -> UIIT_MSG_CREST_REGISTER_FAIL,
2 -> UIIT_MSG_CREST_REGISTER_ERROR) and uploads out of band. The
browser port has no install folder, so the owner's 2026-11-07 contract
supplies the source instead: the master picks a BMP, PNG or JPG in the
browser and the bytes ride the crest upload control lane
(commerce-controls.ts CREST_UPLOAD_CONTROL); the server alone decides
what they are and re-encodes them to the native 16x16 palette form.
The use's 0xB5BD success answer raises the completion box 0x1A/0x1B.

===========================================================================
*/

export const GUILD_CREST_REGISTER_REF_OBJ_ID = 9268;
export const UNION_CREST_REGISTER_REF_OBJ_ID = 9270;

// The owner's hard cap on a picked image, checked before the body is
// read (server crest.MaxCrestImageBytes).
export const CREST_IMAGE_MAX_BYTES = 256 << 10;

export type CrestRegisterKind = "guild" | "union";

/*
================
crestRegisterKind

Which crest a register item stands, or undefined for any other row.
================
*/
export function crestRegisterKind( refObjId: number ): CrestRegisterKind | undefined {
	if ( refObjId === GUILD_CREST_REGISTER_REF_OBJ_ID ) return "guild";
	if ( refObjId === UNION_CREST_REGISTER_REF_OBJ_ID ) return "union";
	return undefined;
}

/*
================
crestRegisterKeys

The flow's lines for one kind: the confirm box's title and body (the
native FILE/FOLDER lines kept verbatim though the browser picker
supersedes the folder convention), the already-standing refusal printed
at OK, the upload lane's fail and error lines, and the completion box's
body with its four information lines. Both confirm boxes take the
generic confirm caption; the union register string is the third body
line of its box, not a title (client message-box composer, kind 0x18).
================
*/
export function crestRegisterKeys( kind: CrestRegisterKind ): {
	readonly title: string;
	readonly lines: readonly string[];
	readonly refused: string;
	readonly fail: string;
	readonly error: string;
	readonly complete: string;
	readonly information: readonly string[];
} {
	const information = [
		"UIIT_MSG_CREST_REGISTER_INFORMATION_01",
		"UIIT_MSG_CREST_REGISTER_INFORMATION_02",
		"UIIT_MSG_CREST_REGISTER_INFORMATION_03",
		"UIIT_MSG_CREST_REGISTER_INFORMATION_04"
	];
	return kind === "guild" ?
		{
			title: "UIIT_STT_CONFIRM_BOX",
			lines: [
				"UIIT_MSG_GUILD_USER_CREST",
				"UIIT_MSG_GUILD_USER_CREST_FILE",
				"UIIT_MSG_GUILD_USER_CREST_FOLDER",
				"UIIT_MSG_GUILD_CREST_REGISTER"
			],
			refused: "UIIT_MSG_CREST_REGISTER_OVERLAP",
			fail: "UIIT_MSG_CREST_REGISTER_FAIL",
			error: "UIIT_MSG_CREST_REGISTER_ERROR",
			complete: "UIIT_MSG_GUILD_CREST_REGISTER_COMPLETE",
			information
		} :
		{
			title: "UIIT_STT_CONFIRM_BOX",
			lines: [
				"UIIT_MSG_UNION_USER_CREST",
				"UIIT_MSG_UNION_USER_CREST_FOLDER",
				"UIIT_CTL_UNION_CREST_REGISTER",
				"UIIT_MSG_UNION_CREST_REGISTER"
			],
			refused: "UIIT_MSG_CREST_REGISTER_OVERLAP",
			fail: "UIIT_MSG_CREST_REGISTER_FAIL",
			error: "UIIT_MSG_CREST_REGISTER_ERROR",
			complete: "UIIT_MSG_UNION_CREST_REGISTER_COMPLETE",
			information
		};
}

/*
================
crestRegisterUploadPayload

One upload on the crest lane: the crest kind, the bag slot holding the
register item, and the picked image's bytes, base64. The character is
the session's own, never the payload's business (server
crestRegisterRequest).
================
*/
export function crestRegisterUploadPayload(
	kind: CrestRegisterKind,
	slot: number,
	image: Uint8Array
): Uint8Array {
	let binary = "";
	// Bytes per fromCharCode chord: stack-safe for a cap-sized image.
	const CHORD = 32768;
	for ( let offset = 0; offset < image.length; offset += CHORD ) {
		binary += String.fromCharCode( ...image.subarray( offset, offset + CHORD ) );
	}
	return new TextEncoder().encode(
		JSON.stringify( { kind, slot, image: btoa( binary ) } )
	);
}

export type CrestRegisterResult =
	| { readonly ok: true; readonly crest: number; }
	| { readonly ok: false; readonly reason: string; readonly code?: number; };

/*
================
crestRegisterResult

The upload lane's one answer (server crestRegisterAnswer), strictly:
a success names the new standing revision; a refusal names its closed
reason and, for the guild and union gates, the native item-use notice
byte (0xBB level, 0xBC master, 0xBD no union). Anything else throws,
as the commerce decoders throw on a malformed frame.
================
*/
export function crestRegisterResult( payload: Uint8Array ): CrestRegisterResult {
	if ( payload.length > 1048576 ) throw Error( "Oversized crest register response" );
	const parsed: unknown = JSON.parse(
		new TextDecoder( "utf-8", { fatal: true } ).decode( payload )
	);
	if ( !parsed || typeof parsed !== "object" || Array.isArray( parsed ) ) {
		throw Error( "Invalid crest register response" );
	}
	const record = parsed as Record<string, unknown>;
	const keys = Object.keys( record ).sort().join( "," );
	if ( record.ok === true ) {
		if (
			keys !== "crest,ok" || typeof record.crest !== "number" ||
			!Number.isInteger( record.crest ) || record.crest < 0 || record.crest > 0xffffffff
		) {
			throw Error( "Invalid crest register response" );
		}
		return { ok: true, crest: record.crest };
	}
	if ( record.ok === false ) {
		if ( typeof record.reason !== "string" || record.reason.length === 0 ) {
			throw Error( "Invalid crest register response" );
		}
		if ( keys === "ok,reason" ) return { ok: false, reason: record.reason };
		if (
			keys === "code,ok,reason" && typeof record.code === "number" &&
			Number.isInteger( record.code ) && record.code >= 0 && record.code <= 255
		) {
			return { ok: false, reason: record.reason, code: record.code };
		}
		throw Error( "Invalid crest register response" );
	}
	throw Error( "Invalid crest register response" );
}
