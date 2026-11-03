/*
===========================================================================

title-status.ts - the title login refusal messages

The refusal table for the internal title opcode 0xff3,
CPSTitle_OnPacketReceived (0x749550; status dispatch table 0x74a0c8).
Transport failures stay distinct from native credential failures; there
is no invented retry count.

Recorded beside the table (#2271): native strings this flow cannot show.

- UIO_MSG_ERROR_ACCOUNT_STOP_BLOCK / _BLOCK_TIME (#2271 PR-A,
verified on the pinned client): the blocked case is this table's
status 3, argument 1 (handler 0x749550, status table 0x74a0c8,
argument table 0x74a108). After the status/argument pair the answer
carries two length-prefixed packet strings (reader 0x4b6870): the
block reason, then the block-end text. A reason containing '#' is
split into structured fields and its datetime is parsed and
converted before display (0x74979d); a plain reason and the end
text append raw. The box heads the ACCOUNT_STOP line with a
"[Block Detail]" caption and formats it "%s\n%s%s\n%s%s"
(0x749a33): ACCOUNT_STOP, STOP_BLOCK + reason, BLOCK_TIME + end.
Our login answer carries only a status and a uint32 argument
(session.ts admits no string field) and the Agent has no
blocked-account refusal to send (handlers.go never emits status 3;
the account authority exposes only id + password hash), so there
is no reason or time to append. Adoption is the #2271 split, not a
client mapping: PR-D stores the block (account scope; reason as
free text; until optional, blank is permanent), PR-B answers
status 3 argument 1 with the two strings only after the password
verifies (wrong-password and unknown-id answers stay identical),
PR-C renders the appended lines here.
- UIO_MSG_ERROR_ACCOUNT_BLOCK_LINK and
UIO_MSG_ERROR_NO_ACCOUNT_INFO_GOTO_HOMEPAGE / _1: retail web
addresses. Never shown, here or natively in this flow (status 3
argument 3 shows THERE_IS_NO_ACCOUNT_INFO, 0x749aa7).
- UIO_MSG_ERROR_VERSION: no version refusal exists in our login/title
flow (the login request carries no client version and the Agent has
no version gate); version skew surfaces as asset-generation load
failures, not a title message.
- UIO_MSG_ERROR_NOT_SERVICE_TIME / _TIME2: maintenance here is the
server list's per-shard state plus status 5 refusals (C5); the native
service-time answer has no producer in the Agent.
- UIO_MSG_ERROR_CITATION_ID: the citation wait shows
UIO_MSG_ERROR_CITATION (ui.ts); the session contract carries no
citation id to key the _ID variant on.
- UIIO_CLIENT_START_CONTENT_FAIL_ACCOUNT_ERR / _CONNECT_ERR: native
client-start content failures; our start failures surface as statuses
11-13 or as transport failures. No producer here.

===========================================================================
*/

/*
================
titleStatusKey

The text key for one login refusal (status, argument), or undefined when
the native table has no message for it. Dispatch table 0x74a0c8 entries
17-20 exist (0x7496ce, 0x749a96, 0x749aa7, 0x749b3d) but the cmp 0xF
guard at 0x749635 makes them unreachable: only statuses 1-16 reach the
table, so 17-20 have no message.
================
*/
export function titleStatusKey( status: number | undefined, argument?: number ): string | undefined {
	switch ( status ) {
		case 2:
			return argument === undefined ? "UIO_MSG_ERROR_PASSWORD" : "UIIT_STT_GLOBAL_PASSWORD_INPUT_ERROR";
		case 3:
			return argument === 1 ?
				"UIO_MSG_ERROR_ACCOUNT_STOP" :
				argument === 2 ?
				"UIO_MSG_ERROR_ACCOUNT_CONNECT_IMPOSSIBILE" :
				argument === 3 ?
				"UIO_MSG_ERROR_THERE_IS_NO_ACCOUNT_INFO" :
				argument === 4 ?
				"UIO_MSG_ERROR_GRATIS_USER_BLOCKED" :
				undefined;
		case 4:
			return "UIO_MSG_ERROR_OVERLAP";
		case 6:
			return "UIO_MSG_ERROR_SERVER_BUSY_CONNECT_IMPOSSIBILE";
		case 5:
		case 7:
		case 8:
		case 9:
		case 10:
			return "UIO_MSG_ERROR_SEVER_CONNECT";
		case 11:
			return "UIO_MSG_ERROR_CONTENT_FAIL_INSUFFICIENT_IP";
		case 12:
			return "UIIO_CLIENT_START_CONTENT_FAIL_BILLING_FAILED";
		case 13:
			return "UIIO_CLIENT_START_CONTENT_FAIL_BILLING_RELATED";
		case 14:
			return "UIIO_SMERR_ADULT_ONLY_SERVER";
		case 15:
			return "UIIO_SMERR_TEENOVER_ONLY_SERVER";
		case 16:
			return "UITT_TEENSERVER_ERRMGS_ADULT";
	}
	// Entries 17-20 exist in the dispatch table 0x74a0c8 but the cmp 0xF
	// guard at 0x749635 makes them unreachable; statuses 17-20 fall
	// through with no message.
}
/*
================
titleStatusMessage

The resolved refusal message: status 2 packs the failed/total password
counters into the argument's two halves (native order), and the connect
failures carry their native (C<status>) suffix.
================
*/
export function titleStatusMessage(
	status: number | undefined,
	argument: number | undefined,
	resolve: ( key: string ) => string
): string | undefined {
	const key = titleStatusKey( status, argument );
	if ( !key ) return;
	let message = resolve( key );
	if ( status === 2 && argument !== undefined ) {
		const values = [ argument & 0xffff, argument >>> 16 ];
		let index = 0;
		message = message.replace( /%d/g, () => String( values[index++] ) );
	}
	if ( status === 5 || status === 7 || status === 8 || status === 9 || status === 10 ) message += `(C${status})`;
	return message;
}

// While no shard is operating (every row native "Check": maintenance, or a
// fleet starting), the open server list asks again this often, so the rows
// turn live without the player closing and reopening the list.
// INFERENCE: the native title's own list refresh was not located; the
// cadence is chosen to be quick next to a restart and light on the Agent.
export const SERVER_CHECK_RETRY_MS = 5000;

/*
================
serverListRefreshDue

The open list's next request time when every listed shard is down, or null
when no refresh is due (a shard is running, the list is closed or empty, or
a request is already in flight).
================
*/
export function serverListRefreshDue(
	servers: readonly { readonly operating: boolean; }[],
	open: boolean,
	pending: boolean,
	now: number,
	retryAt: number
): number | null {
	if ( !open || pending || !servers.length || servers.some( server => server.operating ) ) return null;
	return now >= retryAt ? now + SERVER_CHECK_RETRY_MS : null;
}
