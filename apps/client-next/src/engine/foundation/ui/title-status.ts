// Internal title opcode 0xff3, sub_749550. Transport failures stay distinct
// from native credential failures; there is no invented retry count.
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
}
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
