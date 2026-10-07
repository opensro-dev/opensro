/*
===========================================================================

dialog.ts - the bug report window, its launcher and the Record control

Plain DOM over the canvas, like the FPS chip: the reporter is a tool around
the game, not part of the native interface, and must keep working when
the game's own UI is what broke.

The Record control beside the launcher starts a recording, shows its
time against the server's cap while it runs, and stops it; the reporter
then opens the window with that recording, which the clip trimmer
(trimmer.ts) cuts. The control never keeps keyboard focus: the game
listens on `window`, and a focused button would turn the next Space
into another press. Sending, any
compression and keeping the original are the reporter's; the window shows
their progress and lists the reports saved on this device, each one
exportable as a .zip for the team.

Keyboard events stop at the window: the game listens on `window`, and
typing a description must not walk the character around.

===========================================================================
*/
import type { ReplayState } from "@/engine/contracts/bug-report";
import type { RecordingPhase } from "./recording";
import type { Mp4Track } from "@/engine/foundation/media/mp4";
import type { ArchivedSummary } from "./archive";
import { createClipTrimmer, element, type ClipTrimmer } from "./trimmer";

const MIN_DESCRIPTION = 10;
const MAX_DESCRIPTION = 2000;
const MEGABYTE = 1024 * 1024;
// What the Record control says in each phase (title and accessible name).
const RECORD_LABELS: Record<RecordingPhase, string> = {
	idle: "Record a clip for a bug report (the report's diagnostics already cover the last two minutes of game events)",
	starting: "Starting the recording (click to cancel)",
	recording: "Stop recording and report the bug",
	finishing: "Saving the recording"
};

// The pill's text outside "recording", which shows the time instead.
const RECORD_STATUS: Record<RecordingPhase, string> = {
	idle: "",
	starting: "Starting…",
	recording: "",
	finishing: "Saving…"
};

/*
================
OutgoingReport

The player's report as the window hands it over: the whole replay (kept
on the device) and the selected part (sent), both uncompressed.
================
*/
export interface OutgoingReport {
	readonly description: string;
	readonly replay: Mp4Track | null;
	readonly clip: Mp4Track | null;
	readonly range: { readonly start: number; readonly end: number; } | null;
	readonly screenshot: Blob | null;
}

/*
================
SendOutcome
================
*/
export interface SendOutcome {
	readonly ok: boolean;
	readonly message: string;
}

/*
================
DialogHost

What the window needs from the reporter that owns it.
================
*/
export interface DialogHost {
	deliver( report: OutgoingReport, progress: ( text: string ) => void ): Promise<SendOutcome>;
	still(): Promise<Blob | null>;
	saved(): Promise<ArchivedSummary[]>;
	exportZip( id: string ): Promise<Blob | null>;
	forget( id: string ): Promise<void>;
	launch(): void;
	/** Record pressed while idle. */
	record(): void;
	/** Stop pressed while starting or recording. */
	stopRecording(): void;
}

/*
================
OpenRequest
================
*/
export interface OpenRequest {
	readonly text: string;
	readonly destinations?: readonly string[];
	readonly replay: Mp4Track | null;
	readonly maxBytes: number;
	readonly replayState: ReplayState;
	readonly replayError: string | null;
}

/*
================
deliveryNote

Older Agents do not advertise destinations; avoid guessing their storage.
================
*/
export function deliveryNote( destinations: readonly string[] = [] ): string {
	const discord = destinations.includes( "discord" ), directory = destinations.includes( "directory" );
	if ( discord && directory ) {
		return "The report and its attachments are posted to the team's Discord channel and saved on the game server.";
	}
	if ( discord ) return "The report and its attachments are posted to the team's Discord channel.";
	if ( directory ) return "The report and its attachments are saved on the game server for the team to review.";
	return "The report and its attachments are sent to the team.";
}

/*
================
replayNote

Why the report carries a screenshot instead of a clip, in words the player
can act on: "(Options)" alone read as a setting nobody could find.
================
*/
export function replayNote( state: ReplayState, error: string | null ): string {
	const fallback = "A screenshot will be attached instead.";
	if ( state === "unsupported" ) {
		return `This browser cannot record the game${error ? ` (${error})` : ""}. ` + fallback;
	}
	return "No recording is attached. To attach a video, press the red Record button beside the ! button, " +
		"reproduce the bug, then press Stop. " + fallback;
}

/*
================
BugReportDialog
================
*/
export interface BugReportDialog {
	showLauncher( visible: boolean ): void;
	/** The Record control's phase and, while recording, its seconds against the cap. */
	showRecording( phase: RecordingPhase, seconds: number, maxSeconds: number ): void;
	/** Asks the player whether to download a saved report someone requested. */
	offer( report: ArchivedSummary, from: string ): void;
	/** A one-line message in the same place, for requests that cannot be met. */
	notice( text: string ): void;
	open( request: OpenRequest ): void;
	isOpen(): boolean;
	dispose(): void;
}

/*
================
createBugReportDialog
================
*/
export function createBugReportDialog( host: DialogHost ): BugReportDialog {
	const lifetime = new AbortController();
	const signal = lifetime.signal;
	const launcher = element( "button", "sro-bug-launcher" );
	launcher.type = "button";
	launcher.textContent = "!";
	launcher.title = "Report a bug (/bug)";
	launcher.setAttribute( "aria-label", "Report a bug" );
	launcher.hidden = true;
	launcher.addEventListener( "click", () => host.launch(), { signal } );
	// Inside the FPS chip, under its toggle: the platform moves that chip
	// beside the minimap every frame, and the launcher follows it there.
	const chip = document.getElementById( "fps-chip" );
	if ( chip ) chip.insertBefore( launcher, document.getElementById( "fps-readout" ) );
	else document.body.append( launcher );
	const recordControl = createRecordControl( host, signal );
	launcher.before( recordControl.element );

	let root: HTMLElement | null = null;
	let toast: HTMLElement | null = null;
	let trimmer: ClipTrimmer | null = null;
	let downloads: string[] = [];
	let sending = false;

	/*
	================
	close
	================
	*/
	function close() {
		if ( !root || sending ) return;
		root.remove();
		root = null;
		trimmer?.dispose();
		trimmer = null;
		for ( const url of downloads ) URL.revokeObjectURL( url );
		downloads = [];
	}

	/*
	================
	download
	================
	*/
	function download( blob: Blob, name: string ) {
		const url = URL.createObjectURL( blob );
		downloads.push( url );
		const link = element( "a" );
		link.href = url;
		link.download = name;
		link.click();
	}

	/*
	================
	showToast

	One request at a time, above the game; it stays until answered, and its
	keys stop here like the report window's.
	================
	*/
	function showToast( text: string, actions: readonly HTMLButtonElement[] ) {
		toast?.remove();
		const box = element( "div", "sro-bug-request" );
		box.setAttribute( "role", "alertdialog" );
		box.setAttribute( "aria-label", "Bug report request" );
		const message = element( "p" );
		message.textContent = text;
		const dismiss = element( "button" );
		dismiss.type = "button";
		dismiss.textContent = "Dismiss";
		dismiss.addEventListener( "click", () => box.remove(), { signal } );
		const row = element( "div", "sro-bug-request__actions" );
		row.append( ...actions, dismiss );
		box.append( message, row );
		for ( const kind of [ "keydown", "keyup", "keypress" ] as const ) {
			box.addEventListener( kind, event => event.stopPropagation(), { signal } );
		}
		document.body.append( box );
		toast = box;
	}

	/*
	================
	offer
	================
	*/
	function offer( report: ArchivedSummary, from: string ) {
		const zip = element( "button" );
		zip.type = "button";
		zip.textContent = "Download .zip";
		zip.addEventListener( "click", () => {
			zip.disabled = true;
			zip.textContent = "Preparing…";
			host.exportZip( report.id ).then( blob => {
				if ( blob ) download( blob, report.id + ".zip" );
				toast?.remove();
			}, () => {
				zip.disabled = false;
				zip.textContent = "Download .zip";
			} );
		}, { signal } );
		const who = from ? `${from} asked` : "You were asked";
		showToast(
			`${who} for the full bug report ${report.id} (${new Date( report.createdAt ).toLocaleString()}, ` +
				`${(report.bytes / MEGABYTE).toFixed( 1 )} MB). Download it and send the .zip to the team.`,
			[ zip ]
		);
	}

	/*
	================
	savedList

	The reports kept on this device, newest first, refreshed on demand.
	================
	*/
	function savedList() {
		const section = element( "details", "sro-bug-report__saved" );
		const summary = element( "summary" );
		const list = element( "ul" );
		section.append( summary, list );
		/*
		================
		refresh
		================
		*/
		const refresh = () => {
			host.saved().then( rows => {
				summary.textContent = `Reports saved on this device (${rows.length})`;
				section.hidden = rows.length === 0;
				list.replaceChildren( ...rows.map( row => {
					const item = element( "li" );
					const text = element( "span" );
					text.textContent = `${row.id} · ${new Date( row.createdAt ).toLocaleString()} · ` +
						`${(row.bytes / MEGABYTE).toFixed( 1 )} MB${row.delivered ? "" : " · not sent"} — ` +
						row.description.slice( 0, 60 );
					text.title = row.description;
					const zip = element( "button" ), forget = element( "button" );
					zip.type = forget.type = "button";
					zip.textContent = "Download .zip";
					forget.textContent = "Delete";
					zip.addEventListener( "click", () => {
						zip.disabled = true;
						host.exportZip( row.id ).then( blob => {
							if ( blob ) download( blob, row.id + ".zip" );
							zip.disabled = false;
						}, () => {
							zip.disabled = false;
						} );
					}, { signal } );
					forget.addEventListener( "click", () => {
						host.forget( row.id ).then( refresh, refresh );
					}, { signal } );
					item.append( text, zip, forget );
					return item;
				} ) );
			}, () => {
				section.hidden = true;
			} );
		};
		section.hidden = true;
		refresh();
		return { section, refresh };
	}

	/*
	================
	open
	================
	*/
	function open( request: OpenRequest ) {
		if ( root ) return;
		const replay = request.replay;
		root = element( "div", "sro-bug-report" );
		root.setAttribute( "role", "dialog" );
		root.setAttribute( "aria-modal", "true" );
		root.setAttribute( "aria-labelledby", "sro-bug-report-title" );
		const panel = element( "form", "sro-bug-report__panel" );
		const title = element( "h2", "sro-bug-report__title" );
		title.id = "sro-bug-report-title";
		title.textContent = "Report a bug";
		panel.append( title );

		const clipTrimmer = replay ? createClipTrimmer( replay, request.maxBytes ) : null;
		trimmer = clipTrimmer;
		if ( clipTrimmer ) panel.append( clipTrimmer.element );
		else {
			const note = element( "p", "sro-bug-report__note" );
			note.textContent = replayNote( request.replayState, request.replayError );
			panel.append( note );
		}

		const description = element( "textarea", "sro-bug-report__description" );
		description.maxLength = MAX_DESCRIPTION;
		description.rows = 4;
		description.placeholder = "What happened, and what did you expect to happen?";
		description.value = request.text.trim();
		description.setAttribute( "aria-label", "Description" );
		const privacy = element( "p", "sro-bug-report__note" );
		privacy.textContent = deliveryNote( request.destinations ) + " " +
			"Technical diagnostics include recent movement, input timing, game state and browser details. " +
			"Chat text and account credentials are excluded from those diagnostics. " +
			"The clip may show chat and other players, and includes the game's sound.";
		const status = element( "p", "sro-bug-report__status" );
		status.setAttribute( "role", "status" );
		const actions = element( "div", "sro-bug-report__actions" );
		const cancel = element( "button" ), send = element( "button" );
		cancel.type = "button";
		cancel.textContent = "Cancel";
		send.type = "submit";
		send.textContent = "Send report";
		actions.append( cancel, send );
		const saved = savedList();
		panel.append( description, privacy, status, actions, saved.section );
		root.append( panel );
		document.body.append( root );

		cancel.addEventListener( "click", close, { signal } );
		root.addEventListener( "pointerdown", event => {
			if ( event.target === root ) close();
		}, { signal } );
		for ( const kind of [ "keydown", "keyup", "keypress" ] as const ) {
			root.addEventListener( kind, event => {
				event.stopPropagation();
				if ( kind === "keydown" && (event as KeyboardEvent).key === "Escape" ) close();
			}, { signal } );
		}
		panel.addEventListener( "submit", async event => {
			event.preventDefault();
			if ( sending ) return;
			const text = description.value.trim();
			if ( text.length < MIN_DESCRIPTION ) {
				status.dataset.kind = "error";
				status.textContent = `Please describe the bug (at least ${MIN_DESCRIPTION} characters).`;
				description.focus();
				return;
			}
			sending = true;
			send.disabled = cancel.disabled = true;
			status.dataset.kind = "progress";
			status.textContent = "Preparing the report…";
			const clip = clipTrimmer?.selection() ?? null;
			const outcome = await host.deliver( {
				description: text,
				replay,
				clip,
				range: clip ? clipTrimmer!.range() : null,
				screenshot: clip ? null : await host.still()
			}, progress => {
				status.textContent = progress;
			} );
			sending = false;
			status.dataset.kind = outcome.ok ? "done" : "error";
			status.textContent = outcome.message;
			cancel.disabled = false;
			saved.refresh();
			if ( outcome.ok ) {
				cancel.textContent = "Close";
				send.hidden = true;
				description.disabled = true;
				cancel.focus();
			} else send.disabled = false;
		}, { signal } );
		description.focus();
	}

	return {
		showLauncher( visible ) {
			launcher.hidden = !visible;
			recordControl.element.hidden = !visible;
		},
		showRecording: recordControl.show,
		open,
		offer,
		notice( text ) {
			showToast( text, [] );
		},
		isOpen: () => root !== null,
		dispose() {
			sending = false;
			close();
			toast?.remove();
			toast = null;
			lifetime.abort();
			launcher.remove();
			recordControl.element.remove();
		}
	};
}

/*
================
RecordControl
================
*/
interface RecordControl {
	readonly element: HTMLElement;
	show( phase: RecordingPhase, seconds: number, maxSeconds: number ): void;
}

/*
================
createRecordControl

A round red Record button; while recording it becomes a pill with a
pulsing dot, the elapsed time against the cap, and a square Stop button.
show() runs every frame and writes the DOM only when the phase or the
whole second changes.
================
*/
function createRecordControl( host: DialogHost, signal: AbortSignal ): RecordControl {
	const root = element( "div", "sro-bug-record" );
	root.hidden = true;
	const time = element( "span", "sro-bug-record__time" );
	time.setAttribute( "role", "timer" );
	time.hidden = true;
	const button = element( "button", "sro-bug-record__button" );
	button.type = "button";
	button.append( element( "span", "sro-bug-record__icon" ) );
	root.append( time, button );
	let phase: RecordingPhase = "idle", shown = "";
	button.addEventListener( "click", () => {
		button.blur();
		if ( phase === "idle" ) host.record();
		else if ( phase !== "finishing" ) host.stopRecording();
	}, { signal } );
	show( "idle", 0, 0 );

	/*
	================
	show
	================
	*/
	function show( next: RecordingPhase, seconds: number, maxSeconds: number ) {
		const whole = Math.floor( seconds );
		const key = `${next} ${whole} ${maxSeconds}`;
		if ( key === shown ) return;
		shown = key;
		phase = next;
		root.dataset.phase = next;
		button.title = RECORD_LABELS[next];
		button.setAttribute( "aria-label", RECORD_LABELS[next] );
		button.disabled = next === "finishing";
		time.hidden = next === "idle";
		time.textContent = next === "recording" ?
			`${minutes( whole )} / ${minutes( maxSeconds )}` :
			RECORD_STATUS[next];
	}

	return { element: root, show };
}

/*
================
minutes

Whole seconds as m:ss, the recording timer's format.
================
*/
function minutes( seconds: number ): string {
	const value = Math.max( 0, Math.floor( seconds ) );
	return `${Math.floor( value / 60 )}:${String( value % 60 ).padStart( 2, "0" )}`;
}
