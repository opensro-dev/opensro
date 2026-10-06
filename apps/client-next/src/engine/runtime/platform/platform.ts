/*
===========================================================================

platform.ts - the browser page the runtime runs in

Owns everything the runtime touches outside the canvas pixels: raw pointer
and keyboard input, stored player preferences, the DOM mirror of the GPU
interface, the cursor, and the boot loading overlay with its one-line
account of what is loading and how fast.

===========================================================================
*/

import {
	defaultExtendedQuickslot,
	extendedQuickslotOptions,
	type ExtendedQuickslotOptions
} from "@/engine/foundation/ui/extended-quickslot";
import { chatBlocks } from "@/engine/foundation/gameplay/chat-blocks";
import { defaultVideoOptions, videoOptions, type VideoOptions } from "@/engine/foundation/rendering/video-options";
import { defaultInputOptions, inputOptions, virtualKey, type InputOptions } from "@/engine/foundation/ui/input-options";
import { sightMode, type SightMode } from "@/engine/foundation/rendering/camera-options";
import { initialAudioOptions, audioOptions, type AudioOptions } from "@/engine/foundation/audio/options";
import { cameraWheelDelta } from "@/engine/foundation/rendering/camera-wheel";
import { createTouchCamera, type TouchCameraOutput } from "@/engine/foundation/rendering/touch-camera";
import { gameOptions, initialGameOptions, type GameOptions } from "@/engine/foundation/gameplay/game-options";
import { createUiBridge } from "./ui/ui";
import { createTelemetry } from "./telemetry";
import { createCursor } from "./ui/cursor";
import type { UiEvent } from "@/engine/contracts/ui";
import type { RawInput, WorldClickInput } from "@/engine/contracts/input";
import type { Platform } from "@/engine/contracts/runtime";
import type { AssetProgress } from "@/engine/contracts/assets";
import { loadingDetailText } from "@/engine/foundation/ui/loading-detail";

// How often the loading detail line may change while only its rate moves.
const LOADING_DETAIL_REFRESH_MS = 250;

/*
================
createPlatform

Owns the page: the canvas, raw input, stored preferences, the DOM UI bridge,
the cursor, the boot loading overlay and the diagnostics chips.
================
*/
export function createPlatform(
	canvas: HTMLCanvasElement,
	status: HTMLOutputElement,
	onClose: () => void,
	onInput: ( event: RawInput ) => void,
	onGesture: () => void = () => {},
	onUi: ( event: UiEvent ) => void = () => {},
	blocksUi: ( x: number, y: number ) => boolean = () => false,
	onWorldClick: ( x: number, y: number, click?: WorldClickInput ) => void = () => {},
	onWorldHover: ( point: readonly [number, number] | null ) => void = () => {}
): Platform {
	const lifetime = new AbortController();
	// The canvas CSS box, kept current by a ResizeObserver. Reading
	// clientWidth every frame forces a synchronous layout whenever the UI
	// touched the DOM that frame (a trace showed it among the top costs).
	const canvasBox = { width: canvas.clientWidth, height: canvas.clientHeight };
	const canvasObserver = typeof ResizeObserver === "undefined" ?
		null :
		new ResizeObserver( refreshCanvasBox );
	canvasObserver?.observe( canvas );
	/*
	================
	refreshCanvasBox
	================
	*/
	function refreshCanvasBox(): void {
		canvasBox.width = canvas.clientWidth;
		canvasBox.height = canvas.clientHeight;
	}
	/*
	================
	canvasSize

	The canvas CSS size; read live only where no ResizeObserver exists.
	================
	*/
	function canvasSize(): { readonly width: number; readonly height: number; } {
		if ( !canvasObserver ) refreshCanvasBox();
		return canvasBox;
	}
	const blockKey = "sro:v1150:chatting-blocks:1";
	let localBlocks: readonly string[] = [];
	try {
		const stored = localStorage.getItem( blockKey );
		if ( stored !== null ) localBlocks = chatBlocks( JSON.parse( stored ) );
	} catch ( error ) {
		status.value = "Chatting blocks could not be restored: " + String( error );
	}
	onUi( { kind: "chat-blocks", value: localBlocks } );
	const preferenceKey = "sro:v1150:game-options:1";
	let preferences = initialGameOptions();
	try {
		const stored = localStorage.getItem( preferenceKey );
		if ( stored !== null ) preferences = gameOptions( JSON.parse( stored ) );
	} catch ( error ) {
		status.value = "Game options could not be restored: " + String( error );
	}
	onUi( { kind: "preferences", value: preferences } );
	const audioKey = "sro:v1150:audio-options:1";
	let audio = initialAudioOptions();
	try {
		const stored = localStorage.getItem( audioKey );
		if ( stored !== null ) audio = audioOptions( JSON.parse( stored ) );
	} catch ( error ) {
		status.value = "Audio options could not be restored: " + String( error );
	}
	onUi( { kind: "audio-preferences", value: audio } );
	const cameraKey = "sro:v1150:sight-mode:1";
	let camera: SightMode = 0;
	try {
		const stored = localStorage.getItem( cameraKey );
		if ( stored !== null ) camera = sightMode( JSON.parse( stored ) );
	} catch ( error ) {
		status.value = "Camera option could not be restored: " + String( error );
	}
	onUi( { kind: "camera-preferences", value: camera } );
	const quickslotKey = "sro:v1150:extended-quickslot:1";
	let quickslots = defaultExtendedQuickslot();
	try {
		const stored = localStorage.getItem( quickslotKey );
		if ( stored !== null ) quickslots = extendedQuickslotOptions( JSON.parse( stored ) );
	} catch ( error ) {
		status.value = "Quickslot options could not be restored: " + String( error );
	}
	onUi( { kind: "quickslot-preferences", value: quickslots } );
	const inputKey = "sro:v1150:input-options:1";
	let bindings = defaultInputOptions();
	try {
		const stored = localStorage.getItem( inputKey );
		if ( stored !== null ) bindings = inputOptions( JSON.parse( stored ) );
	} catch ( error ) {
		status.value = "Input options could not be restored: " + String( error );
	}
	onUi( { kind: "input-preferences", value: bindings } );
	const videoKey = "sro:v1150:video-options:1";
	let video = defaultVideoOptions();
	try {
		const stored = localStorage.getItem( videoKey );
		if ( stored !== null ) video = videoOptions( JSON.parse( stored ) );
	} catch ( error ) {
		status.value = "Video options could not be restored: " + String( error );
	}
	onUi( { kind: "video-preferences", value: video } );
	/*
	================
	displayScale

	CSS pixels per UI pixel. One, as in native window mode, unless a chosen
	screen size is larger than the page: then the game area shrinks to fit.
	================
	*/
	function displayScale(): number {
		const size = video.displaySize, css = canvasSize().width;
		return size && css > 0 ? css / size[0] : 1;
	}
	/*
	================
	layoutCanvas

	A chosen screen size is the game area itself, centred on the full-screen
	page with black around it, not a stretched low-resolution image: the
	interface keeps its native pixel size and stays sharp. Without one the
	canvas fills the page.
	================
	*/
	function layoutCanvas() {
		const size = video.displaySize, style = canvas.style;
		if ( !size ) {
			style.position =
				style.left =
				style.top =
				style.width =
				style.height =
					"";
			document.body.style.background = "";
			refreshCanvasBox();
			return;
		}
		const scale = Math.min( 1, innerWidth / size[0], innerHeight / size[1] ),
			width = size[0] * scale,
			height = size[1] * scale;
		style.position = "absolute";
		style.left = (innerWidth - width) / 2 + "px";
		style.top = (innerHeight - height) / 2 + "px";
		style.width = width + "px";
		style.height = height + "px";
		document.body.style.background = "#000";
		// A chosen size takes effect this frame, not after the observer reports.
		refreshCanvasBox();
	}
	layoutCanvas();
	addEventListener( "resize", layoutCanvas, { signal: lifetime.signal } );
	/*
	================
	uiPoint

	A pointer position in UI pixels.
	================
	*/
	function uiPoint( event: { clientX: number; clientY: number; } ): [number, number] {
		const r = canvas.getBoundingClientRect(), scale = displayScale();
		return [ (event.clientX - r.left) / scale, (event.clientY - r.top) / scale ];
	}
	/*
	================
	publishPreferences
	================
	*/
	function publishPreferences( next: GameOptions ) {
		localStorage.setItem( preferenceKey, JSON.stringify( next ) );
		preferences = next;
		onUi( { kind: "preferences", value: next } );
	}
	// A browser can change display mode only during a user gesture. Apply is
	// that gesture; restoring preferences at startup must not request fullscreen.
	let displayRequest = false;
	const syncDisplay = () => {
		if ( lifetime.signal.aborted ) return;
		try {
			publishPreferences( { ...preferences, windowMode: document.fullscreenElement === null } );
		} catch ( error ) {
			status.value = "Display preference could not be saved: " + String( error );
		}
	};
	document.addEventListener( "fullscreenchange", syncDisplay, { signal: lifetime.signal } );
	window.addEventListener( "wheel", event => {
		const [x, y] = uiPoint( event );
		if ( blocksUi( x, y ) ) {
			event.preventDefault();
			onUi( { kind: "scroll", x, y, delta: event.deltaY } );
		}
	}, { signal: lifetime.signal, passive: false } );
	const cursor = createCursor();
	// Release skew: a one-shot "visible again" trigger, and the refresh offer
	// the page shell carries hidden (index.html #update-notice).
	let visibleAgain = false;
	const updateNotice = document.getElementById( "update-notice" );
	updateNotice?.querySelector( "button" )?.addEventListener( "click", () => location.reload(), {
		signal: lifetime.signal
	} );
	document.addEventListener( "visibilitychange", () => {
		if ( document.visibilityState === "visible" ) visibleAgain = true;
	}, { signal: lifetime.signal } );
	const loading = document.getElementById( "startup-loading" );
	const loadingLabel = loading?.querySelector( ".sro-boot-loading__label" );
	const loadingDetail = loading?.querySelector( ".sro-boot-loading__detail" );
	// The detail line's view of the downloads: the latest progress and when the
	// byte count last grew, so a stalled transfer reads as waiting, not hung.
	let assetProgress: AssetProgress | null = null, bytesSeen = -1, bytesGrewAt = 0, detailAt = 0;
	let loadingState = { active: false, error: null as string | null, step: "Starting Silkroad Online" };
	/*
	================
	refreshLoadingDetail

	Asset work advances independently of retained UI snapshots. Refresh the
	visible status from both owners so an unchanged screen cannot freeze it.
	================
	*/
	function refreshLoadingDetail( now: number, force = false ) {
		if ( !loadingDetail || (!loadingState.active && !loadingState.error) ) return;
		if ( !force && now - detailAt < LOADING_DETAIL_REFRESH_MS ) return;
		const detail = loadingState.error ?? loadingDetailText( loadingState.step, assetProgress, now - bytesGrewAt );
		if ( loadingDetail.textContent !== detail ) loadingDetail.textContent = detail;
		detailAt = now;
	}
	// Transfer accounting is developer information, not part of native loading art.
	const transfer = import.meta.env.MODE !== "beta" && new URLSearchParams( location.search ).has( "diagnostics" ) ?
		document.getElementById( "loading-transfer" ) :
		null;
	let transferAt = 0;
	const transferFields = new Map(
		[ "title", "file", "bytes", "speed", "files", "cache", "queue" ].map(
			key => [ key, transfer?.querySelector( `[data-transfer="${key}"]` ) ]
		)
	);
	/*
	================
	transferText
	================
	*/
	function transferText( key: string, text: string ) {
		const node = transferFields.get( key );
		if ( node && node.textContent !== text ) node.textContent = text;
	}
	status.hidden = import.meta.env.MODE === "beta" || !new URLSearchParams( location.search ).has( "diagnostics" );
	const fpsChip = document.getElementById( "fps-chip" );
	const telemetry = createTelemetry();
	window.addEventListener( "pointerdown", onGesture, { signal: lifetime.signal, capture: true } );
	window.addEventListener( "pagehide", onClose, { signal: lifetime.signal } );
	const bridge = createUiBridge(
		canvas,
		onUi,
		() => onInput( { kind: "release", timeMs: performance.timeOrigin + performance.now() } ),
		( code, down ) => {
			if ( virtualKey( code ) === bindings.keys[10] || virtualKey( code ) === bindings.keys[30] ) {
				onInput( {
					kind: "key",
					code,
					down,
					timeMs: performance.timeOrigin + performance.now()
				} );
			}
		},
		displayScale
	);
	window.addEventListener( "pointermove", event => {
		const r = canvas.getBoundingClientRect();
		onWorldHover(
			event.target === canvas && r.width > 0 && r.height > 0 ?
				[ (event.clientX - r.left) / r.width, (event.clientY - r.top) / r.height ] :
				null
		);
	}, { signal: lifetime.signal } );
	window.addEventListener( "blur", () => onWorldHover( null ), { signal: lifetime.signal } );
	document.documentElement.addEventListener( "pointerleave", () => onWorldHover( null ), {
		signal: lifetime.signal
	} );
	let uiPointer = false;
	const timeMs = () => performance.timeOrigin + performance.now();
	// Touch has no camera button: a one-finger drag orbits and a pinch zooms.
	const touchCamera = createTouchCamera();
	const touchInput = ( outputs: readonly TouchCameraOutput[] ) => {
		for ( const output of outputs ) onInput( { ...output, timeMs: timeMs() } );
	};
	const pointer = ( event: PointerEvent ) => {
		if ( uiPointer ) {
			if ( event.type === "pointerup" ) uiPointer = false;
			return;
		}
		const [x, y] = uiPoint( event );
		if ( event.pointerType === "touch" ) {
			touchInput(
				event.type === "pointerup" ?
					touchCamera.up( event.pointerId ) :
					event.type === "pointerdown" ?
					touchCamera.down( event.pointerId, x, y, bindings.mouseMode === 0 ? 2 : 1 ) :
					touchCamera.move( event.pointerId, x, y, bindings.mouseMode === 0 ? 2 : 1 )
			);
			return;
		}
		onInput( {
			kind: "pointer",
			x,
			y,
			buttons: event.buttons,
			timeMs: timeMs()
		} );
	};
	// The game owns pointer gestures on its drawing surface, including RMB
	// camera drag. Browser image menus must not interrupt that gesture.
	canvas.addEventListener( "contextmenu", event => event.preventDefault(), { signal: lifetime.signal } );
	canvas.addEventListener( "dragstart", event => event.preventDefault(), { signal: lifetime.signal } );
	canvas.addEventListener( "pointerdown", event => {
		onGesture();
		uiPointer = blocksUi( ...uiPoint( event ) );
		if ( uiPointer ) {
			onInput( { kind: "release", timeMs: timeMs() } );
			return;
		}
		if ( document.activeElement instanceof HTMLElement ) document.activeElement.blur();
		canvas.setPointerCapture( event.pointerId );
		pointer( event );
	}, { signal: lifetime.signal } );
	for ( const name of [ "pointermove", "pointerup" ] as const ) {
		canvas.addEventListener( name, pointer, { signal: lifetime.signal } );
	}
	// Retail 67CCA0 dispatches movement/selection on WM_LBUTTONDOWN and
	// engagement separately on WM_LBUTTONDBLCLK. Pointer drift cannot cancel
	// an already issued command; button-up does not issue another one.
	// Pointer Events emit pointerdown only for the first held mouse button.
	// mousedown also reports LMB pressed during an existing RMB camera drag.
	canvas.addEventListener( "mousedown", event => {
		const r = canvas.getBoundingClientRect(), x = event.clientX - r.left, y = event.clientY - r.top;
		const blocked = blocksUi( ...uiPoint( event ) );
		if ( event.button === 2 && bindings.mouseMode === 1 && !blocked ) {
			onUi( { kind: "activate", id: "hotbar:0" } );
		}
		if ( event.button === 0 && !blocked && r.width > 0 && r.height > 0 ) {
			onWorldClick( x / r.width, y / r.height, { shift: event.shiftKey, alt: event.altKey } );
		}
	}, { signal: lifetime.signal } );
	canvas.addEventListener( "dblclick", event => {
		const r = canvas.getBoundingClientRect(), x = event.clientX - r.left, y = event.clientY - r.top;
		if ( event.button === 0 && !blocksUi( ...uiPoint( event ) ) && r.width > 0 && r.height > 0 ) {
			onWorldClick( x / r.width, y / r.height, { double: true, shift: event.shiftKey, alt: event.altKey } );
		}
	}, { signal: lifetime.signal } );
	canvas.addEventListener( "pointercancel", event => {
		if ( event.pointerType === "touch" ) touchCamera.up( event.pointerId );
		uiPointer = false;
		onInput( { kind: "release", timeMs: timeMs() } );
	}, { signal: lifetime.signal } );
	window.addEventListener( "keydown", event => {
		if (
			/^F([1-9]|1[0-2])$/.test( event.code ) && event.code !== "F5" && event.code !== "F11" &&
			event.code !== "F12"
		) event.preventDefault();
	}, { signal: lifetime.signal, capture: true } );
	window.addEventListener( "keydown", event => {
		if ( event.isComposing || event.keyCode === 229 ) return;
		const vk = virtualKey( event.code );
		if (
			(vk && bindings.keys.includes( vk )) ||
			(/^F([1-9]|1[0-2])$/.test( event.code ) && event.code !== "F5" && event.code !== "F11" &&
				event.code !== "F12")
		) event.preventDefault();
		if ( !event.repeat ) onGesture();
		if ( !event.repeat ) onUi( { kind: "key", code: event.code, shift: event.shiftKey, ctrl: event.ctrlKey } );
		if ( !event.repeat ) {
			onInput( { kind: "key", code: event.code, down: true, timeMs: timeMs() } );
		}
	}, { signal: lifetime.signal } );
	window.addEventListener(
		"keyup",
		event => onInput( { kind: "key", code: event.code, down: false, timeMs: timeMs() } ),
		{ signal: lifetime.signal }
	);
	window.addEventListener( "blur", () => {
		uiPointer = false;
		onInput( { kind: "release", timeMs: timeMs() } );
	}, { signal: lifetime.signal } );
	canvas.addEventListener( "wheel", event => {
		event.preventDefault();
		if ( blocksUi( ...uiPoint( event ) ) ) return;
		onInput( { kind: "wheel", delta: cameraWheelDelta( event ), timeMs: timeMs() } );
	}, { signal: lifetime.signal, passive: false } );
	const viewport = { width: 1, height: 1 };
	return {
		displayScale,
		/*
		================
		saveVideoOptions
		================
		*/
		saveVideoOptions( value: VideoOptions ) {
			const next = videoOptions( value );
			localStorage.setItem( videoKey, JSON.stringify( next ) );
			video = next;
			layoutCanvas();
			onUi( { kind: "video-preferences", value: next } );
		},
		/*
		================
		saveQuickslotOptions
		================
		*/
		saveQuickslotOptions( value: ExtendedQuickslotOptions ) {
			const next = extendedQuickslotOptions( value );
			localStorage.setItem( quickslotKey, JSON.stringify( next ) );
			onUi( { kind: "quickslot-preferences", value: next } );
		},
		/*
		================
		saveInputOptions
		================
		*/
		saveInputOptions( value: InputOptions ) {
			const next = inputOptions( value );
			localStorage.setItem( inputKey, JSON.stringify( next ) );
			bindings = next;
			onUi( { kind: "input-preferences", value: next } );
		},
		/*
		================
		saveSightMode
		================
		*/
		saveSightMode( value: SightMode ) {
			const next = sightMode( value );
			localStorage.setItem( cameraKey, JSON.stringify( next ) );
			onUi( { kind: "camera-preferences", value: next } );
		},
		/*
		================
		saveAudioOptions
		================
		*/
		saveAudioOptions( value: AudioOptions ) {
			const next = audioOptions( value );
			localStorage.setItem( audioKey, JSON.stringify( next ) );
			onUi( { kind: "audio-preferences", value: next } );
		},
		/*
		================
		saveChatBlocks
		================
		*/
		saveChatBlocks( value: readonly string[] ) {
			const next = chatBlocks( value );
			localStorage.setItem( blockKey, JSON.stringify( next ) );
			onUi( { kind: "chat-blocks", value: next } );
		},
		/*
		================
		saveGameOptions
		================
		*/
		saveGameOptions( value: GameOptions ) {
			const next = gameOptions( value ), change = next.windowMode !== preferences.windowMode;
			publishPreferences( next );
			if ( !change || displayRequest ) return;
			const target = document.documentElement;
			if ( next.windowMode && !document.fullscreenElement || !next.windowMode && document.fullscreenElement ) {
				return;
			}
			displayRequest = true;
			try {
				const request = next.windowMode ? document.exitFullscreen() : target.requestFullscreen();
				void request.catch( error => {
					if ( !lifetime.signal.aborted ) {
						status.value = "Display mode could not be changed: " + String( error );
					}
				} ).finally( () => {
					displayRequest = false;
					syncDisplay();
				} );
			} catch ( error ) {
				displayRequest = false;
				status.value = "Display mode could not be changed: " + String( error );
				syncDisplay();
			}
		},
		canvas,
		presentWorldCursor: cursor.world,
		/*
		================
		presentTelemetry
		================
		*/
		presentTelemetry: telemetry.present,
		diagnosticsActive: telemetry.active,
		/*
		================
		presentUi
		================
		*/
		presentUi( state ) {
			bridge.present( state );
			if ( fpsChip ) {
				const scale = displayScale(),
					right = state.hudCorner ? Math.max( 4, canvasSize().width - state.hudCorner[0] * scale + 6 ) : 8,
					top = state.hudCorner ? Math.max( 4, state.hudCorner[1] * scale ) : 8;
				// Write only changes: a style write invalidates layout every publication.
				if ( fpsChip.style.right !== right + "px" ) {
					fpsChip.style.right = right + "px";
					fpsChip.style.setProperty( "--telemetry-right", right + "px" );
				}
				if ( fpsChip.style.top !== top + "px" ) {
					fpsChip.style.top = top + "px";
					fpsChip.style.setProperty( "--telemetry-top", top + "px" );
				}
			}
			if ( loading ) {
				const active = String( !!(state.loading || state.loadingVisible) ),
					error = String( !!state.loadingError ),
					hidden = String( !(state.loading || state.loadingVisible) );
				const native = String( !state.loading && !!state.loadingVisible );
				if ( loading.dataset.native !== native ) loading.dataset.native = native;
				if ( loading.dataset.active !== active ) loading.dataset.active = active;
				if ( loading.dataset.error !== error ) loading.dataset.error = error;
				if ( loading.getAttribute( "aria-hidden" ) !== hidden ) loading.setAttribute( "aria-hidden", hidden );
			}
			if ( loading instanceof HTMLElement && state.loadingProgress !== undefined ) {
				const progress = String( Math.max( 0, Math.min( 1, state.loadingProgress ) ) );
				if ( loading.style.getPropertyValue( "--loading-progress" ) !== progress ) {
					loading.style.setProperty( "--loading-progress", progress );
				}
			}
			if ( loadingLabel && (state.loading || state.loadingVisible || state.loadingError) ) {
				const label = state.loadingError ? "Unable to finish loading" : "Preparing your journey";
				if ( loadingLabel.textContent !== label ) loadingLabel.textContent = label;
			}
			loadingState = {
				active: !!(state.loading || state.loadingVisible),
				error: state.loadingError ?? null,
				step: state.loadingStatus ?? "Starting Silkroad Online"
			};
			refreshLoadingDetail( performance.now(), true );
		},
		/*
		================
		presentLoading
		================
		*/
		presentLoading( state ) {
			const progress = state.progress, now = performance.now();
			if ( progress && progress.filesActive > 0 && !(assetProgress && assetProgress.filesActive > 0) ) {
				bytesGrewAt = now;
			}
			if ( progress && (progress.bytesRead ?? progress.bytesReceived) !== bytesSeen ) {
				bytesSeen = progress.bytesRead ?? progress.bytesReceived;
				bytesGrewAt = now;
			}
			assetProgress = progress;
			refreshLoadingDetail( now );
			if ( !transfer ) return;
			const changed = transfer.hidden === state.visible;
			transfer.hidden = !state.visible;
			if ( !state.visible || !changed && now - transferAt < 250 ) return;
			transferAt = now;
			transferText( "title", state.title );
			const p = state.progress;
			transfer.dataset.connecting = String( !p );
			transferText(
				"file",
				p?.currentFile ?
					decodeURIComponent( p.currentFile ).split( "/" ).at( -1 )! :
					p ?
					"Preparing graphics and checking cached files" :
					"Waiting for the server to accept your connection"
			);
			transferText( "bytes", p ? `${(p.bytesReceived / 1e6).toFixed( 1 )} MB` : "—" );
			transferText(
				"speed",
				p && p.filesActive && p.bytesPerSecond > 0 ? `${(p.bytesPerSecond / 1e6).toFixed( 1 )} MB/s` : "—"
			);
			transferText( "files", p ? String( p.filesReady ) : "—" );
			transferText( "cache", p ? String( p.cacheHits ) : "—" );
			transferText(
				"queue",
				p ?
					p.filesActive ?
						`${p.filesActive} files being prepared · more may be discovered` :
						"Downloads settled · preparing the scene" :
					"Your character stays here until the server is ready"
			);
		},
		canvasSize,
		/*
		================
		readViewport
		================
		*/
		readViewport() {
			// Always the device pixels the canvas covers: a chosen screen size
			// changes the canvas's CSS box (layoutCanvas), never its sharpness.
			const box = canvasSize();
			viewport.width = Math.max( 1, Math.round( box.width * devicePixelRatio ) );
			viewport.height = Math.max( 1, Math.round( box.height * devicePixelRatio ) );
			return viewport;
		},
		/*
		================
		report
		================
		*/
		report( text, error ) {
			if ( status.textContent !== text ) status.textContent = text;
			if ( /^(Runtime|Renderer|Simulation) failed/.test( text ) ) {
				console.error( "[SRO runtime] " + text, ...(error === undefined ? [] : [ error ]) );
				if ( loading ) {
					loading.dataset.active = "true";
					loading.dataset.error = "true";
					loading.setAttribute( "aria-hidden", "false" );
				}
				if ( loadingLabel ) loadingLabel.textContent = "Unable to finish loading";
				if ( loadingDetail ) loadingDetail.textContent = text;
			}
		},
		/*
		================
		runningEntry
		================
		*/
		runningEntry() {
			// Only a release build loads a content-hashed entry; a development
			// server's /src/bootstrap.ts has nothing to compare against.
			if ( import.meta.env.MODE !== "beta" ) return null;
			const src = document.querySelector( 'script[type="module"][src]' )?.getAttribute( "src" );
			return src ? new URL( src, location.origin ).pathname : null;
		},
		/*
		================
		visibilityReturned
		================
		*/
		visibilityReturned() {
			const returned = visibleAgain;
			visibleAgain = false;
			return returned;
		},
		/*
		================
		presentUpdate
		================
		*/
		presentUpdate( newer ) {
			// The refresh is the player's choice: reloading on our own would
			// drop a live session.
			// Called every frame: write only a change. Assigning hidden, even to its
			// current value, invalidates style (1.8 ms a frame in a trace).
			if ( updateNotice && updateNotice.hidden === newer ) updateNotice.hidden = !newer;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			canvasObserver?.disconnect();
			lifetime.abort();
			bridge.dispose();
			cursor.dispose();
			telemetry.dispose();
		}
	};
}
