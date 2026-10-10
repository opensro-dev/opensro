/*
===========================================================================

telemetry.ts - compact player FPS/ping and opt-in developer diagnostics

The Experimental preference reveals a separate icon, never developer data in
the player readout. Only icon visibility persists; panels start closed. This is
a local presentation preference, not server authorization.

===========================================================================
*/
import type { FrameTelemetry } from "@/engine/contracts/runtime";

/*
================
TelemetryOptions

Platform owns persistence; the console follows the same preference path as
Experimental Confirm. Applying a preference never opens the panel.
================
*/
interface TelemetryOptions {
	readonly enabled: boolean;
	readonly onChange: ( enabled: boolean ) => void;
}

/*
================
DeveloperConsole
================
*/
export interface DeveloperConsole {
	setDiagnostics( enabled: boolean ): boolean;
	dumpMovement(): unknown;
	dumpAssets(): unknown;
}
declare global {
	interface Window {
		sroDebug?: DeveloperConsole;
	}
}

/*
================
createTelemetry
================
*/
export function createTelemetry( options: TelemetryOptions ) {
	const lifetime = new AbortController();
	const chip = document.getElementById( "fps-chip" );
	const fpsToggle = document.getElementById( "fps-toggle" );
	const fpsReadout = document.getElementById( "fps-readout" );
	const toggle = document.createElement( "button" );
	toggle.id = "developer-toggle";
	toggle.className = "sro-fps-chip__toggle";
	toggle.type = "button";
	toggle.textContent = "</>";
	toggle.setAttribute( "aria-controls", "developer-readout" );
	const readout = document.createElement( "div" );
	readout.id = "developer-readout";
	readout.className = "sro-fps-chip__readout sro-developer-readout";
	readout.setAttribute( "role", "region" );
	readout.setAttribute( "aria-label", "Developer diagnostics" );
	let enabled = options.enabled;
	let latest: FrameTelemetry | null = null;
	let movementDump: (() => unknown) | undefined;
	let assetDump: (() => unknown) | undefined;
	chip?.insertBefore( toggle, fpsToggle );
	chip?.append( readout );

	/*
 ================
 setExpanded
 ================
 */
	function setExpanded( developer: boolean, expanded: boolean ) {
		const button = developer ? toggle : fpsToggle;
		const panel = developer ? readout : fpsReadout;
		if ( !button || !panel ) return;
		panel.hidden = !expanded;
		const label = `${expanded ? "Hide" : "Show"} ${developer ? "developer diagnostics" : "FPS and ping"}`;
		button.setAttribute( "aria-expanded", String( expanded ) );
		button.setAttribute( "aria-label", label );
		button.title = label;
		if ( !developer ) {
			chip?.setAttribute( "data-expanded", String( expanded ) );
			button.textContent = expanded ? "x" : "F";
		}
	}

	/*
 ================
 present
 ================
 */
	function present( sample: FrameTelemetry ) {
		latest = sample;
		const ping = sample.pingMs == null ? "—" : String( Math.round( sample.pingMs ) );
		if ( fpsReadout && !fpsReadout.hidden ) {
			fpsReadout.textContent = `${Math.round( sample.fps )} FPS · ${ping} ms`;
		}
		if ( !enabled || readout.hidden ) return;
		const ms = ( value: number ) => `${value.toFixed( value < 10 ? 1 : 0 )} ms`;
		readout.textContent = [
			"Developer diagnostics",
			`${Math.round( sample.fps )} FPS · ${ping} ms ping`,
			`Frame avg / p95: ${ms( sample.frameMs )} / ${ms( sample.p95FrameMs )}`,
			`CPU avg / p95: ${ms( sample.cpuMs )} / ${ms( sample.p95CpuMs )}`,
			`Actors: ${sample.actors} · Draws: ${sample.draws} · Groups: ${sample.visibleGroups}`,
			...sample.build.lines,
			sample.build.detail
		].filter( Boolean ).join( "\n" );
	}

	/*
 ================
 setDiagnostics
 ================
 */
	function setDiagnostics( value: boolean ) {
		enabled = value === true;
		toggle.hidden = !enabled;
		if ( !enabled ) {
			setExpanded( true, false );
			readout.textContent = "";
		}
		return enabled;
	}
	setExpanded( false, false );
	setExpanded( true, false );
	toggle.hidden = !enabled;
	const previous = window.sroDebug;
	const consoleApi = {
		/*
		================
		dumpMovement
		================
		*/
		dumpMovement: () => movementDump?.() ?? null,
		/*
		================
		dumpAssets
		================
		*/
		dumpAssets: () => assetDump?.() ?? null,
		/*
		================
		setDiagnostics
		================
		*/
		setDiagnostics( value: boolean ) {
			options.onChange( value === true );
			return enabled;
		}
	};
	window.sroDebug = consoleApi;
	fpsToggle?.addEventListener( "click", () => {
		const expanded = !!fpsReadout?.hidden;
		setExpanded( true, false );
		setExpanded( false, expanded );
		if ( latest ) present( latest );
	}, { signal: lifetime.signal } );
	toggle.addEventListener( "click", () => {
		const expanded = readout.hidden;
		setExpanded( false, false );
		setExpanded( true, expanded );
		if ( latest ) present( latest );
	}, { signal: lifetime.signal } );
	return {
		/*
		================
		setMovementDump
		================
		*/
		setMovementDump: ( dump: () => unknown ) => {
			movementDump = dump;
		},
		/*
		================
		setAssetDump
		================
		*/
		setAssetDump: ( dump: () => unknown ) => {
			assetDump = dump;
		},
		setDiagnostics,
		present,
		active: () => enabled && !readout.hidden && !document.hidden,
		/*
  ================
  dispose
  ================
  */
		dispose() {
			movementDump = undefined;
			assetDump = undefined;
			lifetime.abort();
			toggle.remove();
			readout.remove();
			setExpanded( false, false );
			if ( window.sroDebug === consoleApi ) {
				if ( previous ) window.sroDebug = previous;
				else delete window.sroDebug;
			}
		}
	};
}
