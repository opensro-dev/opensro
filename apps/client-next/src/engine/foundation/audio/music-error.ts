/*
===========================================================================

music-error.ts - what a music element's error says

The music owner plays each track through an HTMLAudioElement. When the
element fails, the browser names the reason in its MediaError: a code and
a message. This module turns that into the incident text, so a dead audio
output is not filed as a broken track (#4581).

The one case the code alone cannot tell apart: Firefox reports an audio
element whose output sink cannot open as MEDIA_ERR_DECODE, with
NS_ERROR_DOM_MEDIA_MEDIASINK_ERR in the message. Only the message
separates that from a real decode failure.

===========================================================================
*/

// HTMLMediaElement MediaError codes (WHATWG HTML, "media error codes").
export const MEDIA_ERR_ABORTED = 1;
export const MEDIA_ERR_NETWORK = 2;
export const MEDIA_ERR_DECODE = 3;
export const MEDIA_ERR_SRC_NOT_SUPPORTED = 4;

// A decode error whose message names the output sink, not the stream.
const SINK_FAILURE_MARKS = [ "mediasink", "audio sink", "audio output" ] as const;

/*
================
MusicMediaError

The two MediaError fields the text reads; a browser's MediaError has both.
================
*/
export interface MusicMediaError {
	readonly code: number;
	readonly message?: string;
}

/*
================
musicElementError

The incident text for a music element's error event: what failed, the
track, and the browser's own message when it gave one.
================
*/
export function musicElementError( error: MusicMediaError | null | undefined, path: string | undefined ): string {
	const message = error?.message?.trim() ?? "";
	let what: string;
	if ( !error ) {
		what = "Music element failed";
	} else if ( error.code === MEDIA_ERR_DECODE ) {
		what = SINK_FAILURE_MARKS.some( mark => message.toLowerCase().includes( mark ) ) ?
			"Music could not play: the audio output is unavailable" :
			"Music could not be decoded";
	} else if ( error.code === MEDIA_ERR_SRC_NOT_SUPPORTED ) {
		what = "Music format is not supported";
	} else if ( error.code === MEDIA_ERR_NETWORK ) {
		what = "Music stream failed to load";
	} else if ( error.code === MEDIA_ERR_ABORTED ) {
		what = "Music load was aborted";
	} else {
		what = `Music element failed (code ${error.code})`;
	}
	return `${what}: ${path ?? "unknown track"}${message ? ` (${message})` : ""}`;
}
