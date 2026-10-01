/*
===========================================================================

trimmer.ts - choosing the part of the replay a bug report attaches

A timeline of the whole replay (a strip of thumbnails) with the selected
part lit and the rest dimmed, two handles and a playhead. The video above
plays the selected part in a loop, with sound.

One video element plays the whole replay for the window's lifetime and
never changes source: swapping sources blanks the picture, which made
every adjustment flicker. Looping inside [start, end] shows the same
frames the attached clip will hold, because the start handle snaps to key
frames (an H.264 clip cut without re-encoding can only begin on one) and
the clip is cut from these very samples when the report is sent.

===========================================================================
*/
import { muxMp4, type Mp4Track } from "@/engine/foundation/media/mp4";
import {
	replayClipTrack,
	replayKeyTimes,
	replaySnapStart,
	replayTrackBytes
} from "@/engine/foundation/media/replay-window";

const MIN_CLIP_SECONDS = 1;
const DEFAULT_CLIP_SECONDS = 30;
const THUMBNAILS = 10;
const THUMBNAIL_WIDTH = 128;
const THUMBNAIL_HEIGHT = 72;
const END_STEP_SECONDS = 0.5;
// Seeks that land this close to the loop's end count as reaching it.
const LOOP_EPSILON = 0.04;
const MEGABYTE = 1024 * 1024;
const PRESETS: readonly (readonly [string, number])[] = [
	[ "Last 10 s", 10 ],
	[ "Last 30 s", 30 ],
	[ "Whole replay", Infinity ]
];

/*
================
ClipTrimmer
================
*/
export interface ClipTrimmer {
	readonly element: HTMLElement;
	/** The selected part, uncompressed, or null when the player unticked it. */
	selection(): Mp4Track | null;
	/** The selection in seconds from the start of the replay. */
	range(): { readonly start: number; readonly end: number; };
	dispose(): void;
}

/*
================
createClipTrimmer
================
*/
export function createClipTrimmer( replay: Mp4Track, maxBytes: number ): ClipTrimmer {
	const lifetime = new AbortController();
	const signal = lifetime.signal;
	const base = replay.samples[0]!.timestampUs;
	const total = Math.max( MIN_CLIP_SECONDS, (replay.samples[replay.samples.length - 1]!.timestampUs - base) / 1e6 );
	const keys = replayKeyTimes( replay.samples );
	const fullUrl = URL.createObjectURL( new Blob( [ muxMp4( replay ) as BlobPart ], { type: "video/mp4" } ) );
	let end = total;
	let start = replaySnapStart( keys, total - DEFAULT_CLIP_SECONDS, end, MIN_CLIP_SECONDS );
	let dragging: "start" | "end" | "both" | null = null;
	let dragOffset = 0;

	const root = element( "div", "sro-trimmer" );
	const caption = element( "p", "sro-trimmer__caption" );
	const stage = element( "div", "sro-trimmer__stage" );
	const video = element( "video", "sro-trimmer__video" );
	video.playsInline = true;
	video.preload = "auto";
	video.src = fullUrl;
	const controls = element( "div", "sro-trimmer__controls" );
	const play = element( "button", "sro-trimmer__play" ), mute = element( "button", "sro-trimmer__play" );
	play.type = mute.type = "button";
	controls.append( play, mute );
	stage.append( video, controls );

	const track = element( "div", "sro-trimmer__track" );
	const strip = element( "div", "sro-trimmer__strip" );
	const shadeLeft = element( "div", "sro-trimmer__shade" ), shadeRight = element( "div", "sro-trimmer__shade" );
	const selected = element( "div", "sro-trimmer__window" );
	const startHandle = handle( "Clip start" ), endHandle = handle( "Clip end" );
	const playhead = element( "div", "sro-trimmer__playhead" );
	track.append( strip, shadeLeft, shadeRight, selected, startHandle, endHandle, playhead );
	const scale = element( "div", "sro-trimmer__scale" );
	const scaleStart = element( "span" ), scaleEnd = element( "span" );
	scaleStart.textContent = clock( 0 );
	scaleEnd.textContent = clock( total );
	scale.append( scaleStart, scaleEnd );

	const footer = element( "div", "sro-trimmer__footer" );
	// Not <output>: index.html pins every <output> as the diagnostics readout.
	const info = element( "div", "sro-trimmer__info" );
	info.setAttribute( "role", "status" );
	const presets = element( "div", "sro-trimmer__presets" );
	for ( const [label, seconds] of PRESETS ) {
		const button = element( "button" );
		button.type = "button";
		button.textContent = label;
		button.addEventListener( "click", () => {
			end = total;
			start = replaySnapStart( keys, total - seconds, end, MIN_CLIP_SECONDS );
			settle();
		}, { signal } );
		presets.append( button );
	}
	footer.append( info, presets );
	const attachLabel = element( "label", "sro-trimmer__attach" );
	const attach = element( "input" );
	attach.type = "checkbox";
	attach.checked = true;
	const attachText = element( "span" );
	attachText.textContent = "Attach this clip to the report";
	attachLabel.append( attach, attachText );
	root.append( caption, stage, track, scale, footer, attachLabel );

	/*
	================
	micros

	Seconds back to the replay's integer microseconds. Rounding keeps a key
	frame's own time on that frame (8039200 / 1e6 * 1e6 is 8039199.999…).
	================
	*/
	function micros( seconds: number ) {
		return base + Math.round( seconds * 1e6 );
	}

	/*
	================
	percent
	================
	*/
	function percent( seconds: number ) {
		return `${(seconds / total * 100).toFixed( 3 )}%`;
	}

	/*
	================
	layout

	Positions the window, shades and handles, and describes the clip.
	================
	*/
	function layout() {
		shadeLeft.style.left = "0";
		shadeLeft.style.width = percent( start );
		shadeRight.style.left = percent( end );
		shadeRight.style.right = "0";
		selected.style.left = percent( start );
		selected.style.width = percent( end - start );
		startHandle.style.left = percent( start );
		endHandle.style.left = percent( end );
		startHandle.setAttribute( "aria-valuenow", start.toFixed( 1 ) );
		startHandle.setAttribute( "aria-valuetext", clock( start ) );
		endHandle.setAttribute( "aria-valuenow", end.toFixed( 1 ) );
		endHandle.setAttribute( "aria-valuetext", clock( end ) );
		const bytes = replayTrackBytes( replayClipTrack( replay, micros( start ), micros( end ) ) );
		const compress = bytes > maxBytes;
		root.dataset.compress = String( compress );
		info.textContent = `Clip ${clock( start )} → ${clock( end )} · ${(end - start).toFixed( 1 )} s · ` +
			`${(bytes / MEGABYTE).toFixed( 1 )} MB` +
			(compress ?
				` → compressed to ${(maxBytes / MEGABYTE).toFixed( 0 )} MB for Discord (full quality kept here)` :
				"");
	}

	/*
	================
	settle

	The selection stopped changing: play it from its start, looping.
	================
	*/
	function settle() {
		layout();
		root.dataset.scrubbing = "false";
		caption.textContent = "Preview of the clip that will be sent (loops)";
		video.currentTime = start;
		void video.play().catch( () => {} );
	}

	/*
	================
	scrub

	While a handle moves, hold the picture at that moment.
	================
	*/
	function scrub( seconds: number ) {
		root.dataset.scrubbing = "true";
		caption.textContent = "Release to preview the clip";
		if ( !video.paused ) video.pause();
		if ( !video.seeking ) video.currentTime = seconds;
		playhead.style.left = percent( seconds );
	}

	/*
	================
	loop

	Per presented frame: keep playback inside the selection.
	================
	*/
	function loop() {
		if ( signal.aborted ) return;
		video.requestVideoFrameCallback( loop );
		if ( dragging ) return;
		const at = video.currentTime;
		if ( !video.paused && (at >= end - LOOP_EPSILON || at < start - LOOP_EPSILON) ) video.currentTime = start;
		playhead.style.left = percent( Math.min( end, Math.max( start, at ) ) );
	}

	/*
	================
	pointerSeconds
	================
	*/
	function pointerSeconds( event: PointerEvent ) {
		const rect = track.getBoundingClientRect();
		return Math.min( total, Math.max( 0, (event.clientX - rect.left) / Math.max( 1, rect.width ) * total ) );
	}

	/*
	================
	moveTo

	Applies a drag of the active handle (or of the whole window) to `seconds`.
	================
	*/
	function moveTo( seconds: number ) {
		if ( dragging === "start" ) {
			start = replaySnapStart( keys, seconds, end, MIN_CLIP_SECONDS );
			scrub( start );
		} else if ( dragging === "end" ) {
			end = Math.min( total, Math.max( start + MIN_CLIP_SECONDS, seconds ) );
			scrub( end );
		} else if ( dragging === "both" ) {
			const length = end - start;
			const wanted = Math.min( total - length, Math.max( 0, seconds - dragOffset ) );
			start = replaySnapStart( keys, wanted, total, MIN_CLIP_SECONDS );
			end = Math.min( total, start + length );
			scrub( start );
		}
		layout();
	}

	track.addEventListener( "pointerdown", event => {
		if ( event.button !== 0 ) return;
		const seconds = pointerSeconds( event );
		if ( event.target === startHandle ) dragging = "start";
		else if ( event.target === endHandle ) dragging = "end";
		else if ( seconds > start && seconds < end ) {
			dragging = "both";
			dragOffset = seconds - start;
		} else dragging = Math.abs( seconds - start ) < Math.abs( seconds - end ) ? "start" : "end";
		track.setPointerCapture( event.pointerId );
		event.preventDefault();
		moveTo( seconds );
	}, { signal } );
	track.addEventListener( "pointermove", event => {
		if ( dragging ) moveTo( pointerSeconds( event ) );
	}, { signal } );
	for ( const kind of [ "pointerup", "pointercancel" ] as const ) {
		track.addEventListener( kind, () => {
			if ( !dragging ) return;
			dragging = null;
			settle();
		}, { signal } );
	}
	startHandle.addEventListener( "keydown", event => {
		const step = event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0;
		if ( !step ) return;
		event.preventDefault();
		const index = keys.findIndex( key => key >= start - 1e-6 ) + step;
		start = replaySnapStart(
			keys,
			keys[Math.max( 0, Math.min( keys.length - 1, index ) )] ?? 0,
			end,
			MIN_CLIP_SECONDS
		);
		settle();
	}, { signal } );
	endHandle.addEventListener( "keydown", event => {
		const step = event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0;
		if ( !step ) return;
		event.preventDefault();
		end = Math.min( total, Math.max( start + MIN_CLIP_SECONDS, end + step * END_STEP_SECONDS ) );
		settle();
	}, { signal } );

	/*
	================
	labelButtons
	================
	*/
	const labelButtons = () => {
		play.textContent = video.paused ? "▶" : "❚❚";
		play.setAttribute( "aria-label", video.paused ? "Play clip" : "Pause clip" );
		mute.textContent = video.muted ? "🔇" : "🔊";
		mute.setAttribute( "aria-label", video.muted ? "Unmute" : "Mute" );
	};
	play.addEventListener( "click", () => {
		if ( video.paused ) void video.play().catch( () => {} );
		else video.pause();
	}, { signal } );
	mute.addEventListener( "click", () => {
		video.muted = !video.muted;
	}, { signal } );
	video.addEventListener( "click", () => play.click(), { signal } );
	for ( const kind of [ "play", "pause", "volumechange" ] as const ) {
		video.addEventListener( kind, labelButtons, { signal } );
	}
	attach.addEventListener( "change", () => {
		root.dataset.detached = String( !attach.checked );
	}, { signal } );
	video.addEventListener( "loadedmetadata", () => settle(), { signal, once: true } );

	drawThumbnails( fullUrl, strip, total, signal );
	video.requestVideoFrameCallback( loop );
	labelButtons();
	layout();

	return {
		element: root,
		selection: () => attach.checked ? replayClipTrack( replay, micros( start ), micros( end ) ) : null,
		range: () => ({ start, end }),
		dispose() {
			lifetime.abort();
			video.pause();
			video.removeAttribute( "src" );
			video.load();
			URL.revokeObjectURL( fullUrl );
		}
	};
}

/*
================
drawThumbnails

Fills the strip with frames sampled evenly across the replay, using a
second, never shown video so the preview keeps playing meanwhile.
================
*/
function drawThumbnails( url: string, strip: HTMLElement, total: number, signal: AbortSignal ) {
	const canvases = Array.from( { length: THUMBNAILS }, () => {
		const canvas = element( "canvas" );
		canvas.width = THUMBNAIL_WIDTH;
		canvas.height = THUMBNAIL_HEIGHT;
		strip.append( canvas );
		return canvas;
	} );
	const source = element( "video" );
	source.muted = true;
	source.preload = "auto";
	source.src = url;
	source.addEventListener( "loadeddata", () => void paintThumbnails( source, canvases, total, signal ), {
		signal,
		once: true
	} );
}

/*
================
paintThumbnails
================
*/
async function paintThumbnails(
	source: HTMLVideoElement,
	canvases: readonly HTMLCanvasElement[],
	total: number,
	signal: AbortSignal
) {
	for ( const [index, canvas] of canvases.entries() ) {
		if ( signal.aborted ) break;
		source.currentTime = (index + 0.5) * total / canvases.length;
		await new Promise<void>( resolve => source.addEventListener( "seeked", () => resolve(), { once: true } ) );
		canvas.getContext( "2d" )?.drawImage( source, 0, 0, canvas.width, canvas.height );
	}
	source.removeAttribute( "src" );
	source.load();
}

/*
================
handle
================
*/
function handle( label: string ) {
	const node = element( "div", "sro-trimmer__handle" );
	node.tabIndex = 0;
	node.setAttribute( "role", "slider" );
	node.setAttribute( "aria-label", label );
	return node;
}

/*
================
element
================
*/
export function element<K extends keyof HTMLElementTagNameMap>( tag: K, className = "" ): HTMLElementTagNameMap[K] {
	const node = document.createElement( tag );
	if ( className ) node.className = className;
	return node;
}

/*
================
clock

Seconds as m:ss.s.
================
*/
export function clock( seconds: number ) {
	const value = Math.max( 0, seconds );
	return `${Math.floor( value / 60 )}:${(value % 60).toFixed( 1 ).padStart( 4, "0" )}`;
}
