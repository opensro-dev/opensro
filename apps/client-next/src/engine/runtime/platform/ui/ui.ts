/*
===========================================================================

ui.ts - the DOM mirror of the GPU-drawn interface

The game draws every window on the canvas. This bridge keeps one real DOM
element per interactive control (a button, a text edit, a range, a hit
region) positioned over its drawn rect, so the browser supplies hit testing,
keyboard focus, text entry, IME and accessibility. It reports what the user
does as UiEvents and applies the focus requests the UI publishes.

A locked text edit is read-only rather than disabled: the browser drops
focus from a disabled element and never returns it.

===========================================================================
*/
import type { UiBridge, UiControl, UiEvent, UiSemantics } from "@/engine/contracts/ui";
/*
================
createUiBridge

One keyed semantic tree. It mirrors interactive controls, never NPC markers.
================
*/
export function createUiBridge(
	canvas: HTMLCanvasElement,
	emit: ( event: UiEvent ) => void,
	release: () => void,
	heldKey: ( code: string, down: boolean ) => void = () => {}
): UiBridge {
	const lifetime = new AbortController(),
		root = document.createElement( "section" ),
		message = document.createElement( "p" );
	root.setAttribute( "aria-label", "Game interface" );
	root.dataset.gpuUi = "semantics";
	root.style.cssText = "position:fixed;inset:0;pointer-events:none;overflow:hidden";
	message.setAttribute( "role", "status" );
	message.style.cssText = "position:absolute;width:1px;height:1px;clip-path:inset(50%);overflow:hidden";
	root.append( message );
	document.body.append( root );
	const controls = new Map<
		string,
		{ element: HTMLInputElement | HTMLButtonElement | HTMLDivElement; value: UiControl; }
	>();
	let composing = false;
	let drag: { id: string; pointer: number; x: number; y: number; moved?: boolean; } | null = null, focusRevision = -1;
	let suppressClick: string | null = null;
	let rightPressed = false;
	window.addEventListener( "pointermove", event => {
		if ( !drag || event.pointerId !== drag.pointer ) return;
		const current = controls.get( drag.id )?.value;
		if ( !current?.draggable || current.disabled ) {
			drag = null;
			emit( { kind: "press", id: null } );
			return;
		}
		const dx = event.clientX - drag.x, dy = event.clientY - drag.y;
		drag = { ...drag, x: event.clientX, y: event.clientY, moved: drag.moved || dx !== 0 || dy !== 0 };
		emit( { kind: "drag", id: drag.id, dx, dy } );
	}, { signal: lifetime.signal } );
	window.addEventListener( "pointerup", event => {
		if ( drag && event.pointerId !== drag.pointer ) return;
		const completed = drag;
		drag = null;
		if ( completed?.moved ) {
			suppressClick = completed.id;
			emit( { kind: "drag-end", id: completed.id, x: event.clientX, y: event.clientY } );
		}
	}, { signal: lifetime.signal } );
	window.addEventListener( "pointercancel", () => {
		drag = null;
		suppressClick = null;
		emit( { kind: "press", id: null } );
	}, { signal: lifetime.signal } );
	window.addEventListener( "blur", () => {
		drag = null;
	}, { signal: lifetime.signal } );
	function edit( element: HTMLInputElement, id: string ) {
		emit( {
			kind: "edit",
			id,
			value: element.value,
			start: element.selectionStart ?? 0,
			end: element.selectionEnd ?? 0,
			composing
		} );
	}
	root.addEventListener( "focusin", event => {
		release();
		const el = event.target as HTMLElement;
		emit( { kind: "focus", id: el.dataset.uiId ?? null } );
	}, { signal: lifetime.signal } );
	root.addEventListener( "focusout", () => {
		composing = false;
		emit( { kind: "focus", id: null } );
	}, { signal: lifetime.signal } );
	root.addEventListener( "keydown", event => {
		event.stopPropagation();
		if ( event.isComposing || composing || event.keyCode === 229 ) return;
		if (
			/^F([1-9]|1[0-2])$/.test( event.code ) && event.code !== "F5" && event.code !== "F11" &&
			event.code !== "F12"
		) event.preventDefault();
		if ( event.code === "Backquote" && event.shiftKey ) {
			event.preventDefault();
			if ( !event.repeat ) emit( { kind: "key", code: event.code, shift: true, ctrl: event.ctrlKey } );
			return;
		}
		if (
			current( event.target )?.value.id === "gm-input" && (event.code === "ArrowUp" || event.code === "ArrowDown")
		) {
			event.preventDefault();
			emit( { kind: "key", code: event.code, shift: event.shiftKey, ctrl: event.ctrlKey } );
			return;
		}
		if ( current( event.target )?.value.captureKeys ) {
			event.preventDefault();
			if ( !event.repeat ) emit( { kind: "key", code: event.code, shift: event.shiftKey, ctrl: event.ctrlKey } );
			return;
		}
		if ( !(event.target instanceof HTMLInputElement) && !event.repeat ) heldKey( event.code, true );
		if (
			event.code !== "Escape" && event.code !== "Space" && event.key !== "Enter" &&
			!(event.target instanceof HTMLInputElement) && !event.repeat
		) emit( { kind: "key", code: event.code, shift: event.shiftKey, ctrl: event.ctrlKey } );
		if ( event.code === "Escape" && !event.repeat ) {
			emit( { kind: "key", code: event.code, shift: event.shiftKey, ctrl: event.ctrlKey } );
			(event.target as HTMLElement).blur();
		}
		if ( event.key === "Enter" && !event.repeat && event.target instanceof HTMLInputElement ) {
			event.preventDefault();
			emit( { kind: "activate", id: "submit" } );
		}
	}, { signal: lifetime.signal } );
	root.addEventListener( "keyup", event => {
		event.stopPropagation();
		heldKey( event.code, false );
	}, { signal: lifetime.signal } );
	window.addEventListener( "pointerup", event => {
		if ( !drag || event.pointerId === drag.pointer ) emit( { kind: "press", id: null } );
	}, { signal: lifetime.signal } );
	window.addEventListener( "blur", () => {
		emit( { kind: "press", id: null } );
		emit( { kind: "hover", id: null } );
	}, { signal: lifetime.signal } );
	// Retail editors own selection, not Chromium's native text drag/drop loop.
	// Dragging selected transparent text otherwise steals pointer delivery and
	// replaces the game cursor with the browser/OS drag image.
	root.addEventListener( "dragstart", event => event.preventDefault(), { signal: lifetime.signal } );
	// The semantic controls overlay the canvas, so canvas context-menu handling
	// cannot see their RMB gestures. Cover editors, disabled controls, regions
	// and future controls here without consuming the game's pointer events.
	root.addEventListener( "contextmenu", event => event.preventDefault(), { signal: lifetime.signal, capture: true } );
	root.addEventListener( "compositionstart", () => {
		composing = true;
	}, { signal: lifetime.signal } );
	root.addEventListener( "compositionend", event => {
		composing = false;
		if ( event.target instanceof HTMLInputElement ) edit( event.target, event.target.dataset.uiId! );
	}, { signal: lifetime.signal } );
	root.addEventListener( "input", event => {
		if ( event.target instanceof HTMLInputElement ) edit( event.target, event.target.dataset.uiId! );
	}, { signal: lifetime.signal } );
	document.addEventListener( "selectionchange", () => {
		const el = document.activeElement;
		if ( el instanceof HTMLInputElement && root.contains( el ) ) edit( el, el.dataset.uiId! );
	}, { signal: lifetime.signal } );
	// Event delegation resolves current state, never the descriptor from creation.
	function current( target: EventTarget | null ) {
		if ( !(target instanceof Element) ) return;
		const el = target.closest<HTMLElement>( "[data-ui-id]" );
		if ( !el || !root.contains( el ) ) return;
		const slot = controls.get( el.dataset.uiId! );
		return slot?.element === el ? slot : undefined;
	}
	root.addEventListener( "pointerdown", event => {
		if ( event.button === 2 ) rightPressed = !!current( event.target )?.value.rightActivate;
	}, { signal: lifetime.signal } );
	window.addEventListener( "pointerup", event => {
		if ( event.button !== 2 ) return;
		const armed = rightPressed;
		rightPressed = false;
		const slot = current( event.target );
		if ( armed && slot?.value.rightActivate && !slot.value.disabled ) {
			emit( { kind: "right-activate", id: slot.value.id } );
		}
	}, { signal: lifetime.signal } );
	window.addEventListener( "pointercancel", () => {
		rightPressed = false;
	}, { signal: lifetime.signal } );
	window.addEventListener( "blur", () => {
		rightPressed = false;
	}, { signal: lifetime.signal } );
	root.addEventListener( "click", event => {
		const slot = current( event.target ), suppressed = suppressClick;
		suppressClick = null;
		if ( slot?.value.kind === "button" && !slot.value.disabled && slot.value.id !== suppressed ) {
			emit( {
				kind: "activate",
				id: slot.value.id,
				shift: event.shiftKey,
				ctrl: event.ctrlKey,
				alt: event.altKey
			} );
		}
	}, { signal: lifetime.signal } );
	root.addEventListener( "dblclick", event => {
		const slot = current( event.target );
		if ( slot?.value.kind === "button" && !slot.value.disabled ) {
			emit( {
				kind: "double-activate",
				id: slot.value.id,
				shift: event.shiftKey,
				ctrl: event.ctrlKey,
				alt: event.altKey
			} );
		}
	}, { signal: lifetime.signal } );
	root.addEventListener( "keydown", event => {
		const slot = current( event.target );
		if (
			slot?.value.kind === "button" && !slot.value.captureKeys && !slot.value.disabled && !event.repeat &&
			(event.code === "Space" || event.key === "Enter")
		) emit( { kind: "press", id: slot.value.id } );
	}, { signal: lifetime.signal } );
	root.addEventListener( "keyup", () => emit( { kind: "press", id: null } ), { signal: lifetime.signal } );
	root.addEventListener( "pointerover", event => {
		const slot = current( event.target );
		if ( slot !== current( event.relatedTarget ) ) {
			emit( { kind: "hover", id: slot && !slot.value.disabled ? slot.value.id : null } );
		}
	}, { signal: lifetime.signal } );
	root.addEventListener( "pointerout", event => {
		const next = current( event.relatedTarget );
		if ( current( event.target ) !== next ) {
			emit( { kind: "hover", id: next && !next.value.disabled ? next.value.id : null } );
			emit( { kind: "press", id: null } );
		}
	}, { signal: lifetime.signal } );
	root.addEventListener( "pointerdown", event => {
		suppressClick = null;
		const slot = current( event.target );
		if (
			event.button !== 0 || !slot || slot.value.disabled || slot.value.kind === "region" && !slot.value.draggable
		) return;
		emit( { kind: "press", id: slot.value.id } );
		if ( slot.value.draggable ) {
			event.preventDefault();
			release();
			slot.element.setPointerCapture( event.pointerId );
			drag = { id: slot.value.id, pointer: event.pointerId, x: event.clientX, y: event.clientY };
		}
	}, { signal: lifetime.signal } );
	function retire( id: string ) {
		const slot = controls.get( id );
		if ( !slot ) return;
		if ( drag?.id === id ) {
			if ( slot.element.hasPointerCapture( drag.pointer ) ) slot.element.releasePointerCapture( drag.pointer );
			drag = null;
			emit( { kind: "press", id: null } );
		}
		if ( document.activeElement === slot.element ) slot.element.blur();
		slot.element.remove();
		controls.delete( id );
	}
	return {
		present( state: UiSemantics ) {
			if ( lifetime.signal.aborted ) return;
			// Identity belongs to the control instance, not its action. Two native
			// controls may invoke the same close handler but must keep separate hits.
			// Validate the whole publication before touching the previous semantic tree.
			const identities = new Set<string>();
			for ( const control of state.controls ) {
				if ( identities.has( control.id ) ) throw Error( "Duplicate UI control identity: " + control.id );
				identities.add( control.id );
			}
			if ( root.getAttribute( "aria-label" ) !== state.title ) root.setAttribute( "aria-label", state.title );
			if ( message.textContent !== state.message ) message.textContent = state.message;
			const wanted = new Set( state.controls.map( c => c.id ) );
			for ( const id of controls.keys() ) if ( !wanted.has( id ) ) retire( id );
			const box = canvas.getBoundingClientRect();
			for ( const [order, control] of state.controls.entries() ) {
				let slot = controls.get( control.id );
				if ( slot && slot.value.kind !== control.kind ) {
					retire( control.id );
					slot = undefined;
				}
				if ( !slot ) {
					const element = control.kind === "region" ?
						document.createElement( "div" ) :
						control.kind === "button" ?
						document.createElement( "button" ) :
						document.createElement( "input" );
					element.dataset.uiId = control.id;
					// Transparent browser editors preserve native IME candidate positioning and selection.
					// Forced-colors users receive visible native controls; GPU focus remains the normal visual.
					element.className = "gpu-ui-control";
					element.style.cssText =
						"position:absolute;pointer-events:auto;box-sizing:border-box;overflow:hidden;white-space:nowrap;min-width:0;min-height:0;margin:0;padding:0;border:0;opacity:0;color:transparent;background:transparent;font:12px Arial;text-align:center;cursor:inherit;";
					if ( element instanceof HTMLInputElement ) {
						element.type = control.kind === "password" ?
							"password" :
							control.kind === "range" ?
							"range" :
							"text";
						element.maxLength = control.maxLength ?? (control.kind === "password" ? 1024 : 256);
						element.autocomplete = "off";
						element.spellcheck = false;
						element.setAttribute( "autocapitalize", "off" );
						element.setAttribute( "autocorrect", "off" );
					}
					root.append( element );
					slot = { element, value: control };
					controls.set( control.id, slot );
				}
				const el = slot.element;
				if ( el.style.zIndex !== String( order ) ) el.style.zIndex = String( order );
				if ( el instanceof HTMLInputElement ) {
					const limit = control.maxLength ?? (control.kind === "password" ? 1024 : 256);
					if ( el.maxLength !== limit ) el.maxLength = limit;
				}
				// Match authored edit hit bounds while retaining its text client insets.
				if ( el instanceof HTMLInputElement ) {
					const align = control.textAlign ?? "center", insets = control.textInsets ?? [ 0, 0, 0, 0 ];
					if ( el.style.textAlign !== align ) el.style.textAlign = align;
					for (
						const [key, n] of [ [ "paddingLeft", insets[0] ], [ "paddingTop", insets[1] ], [
							"paddingRight",
							insets[2]
						], [ "paddingBottom", insets[3] ] ] as const
					) {
						const value = n + "px";
						if ( el.style[key] !== value ) el.style[key] = value;
					}
				}
				if ( el instanceof HTMLInputElement && control.kind === "range" ) {
					const min = String( control.min ?? 0 ), max = String( control.max ?? 0 );
					if ( el.min !== min ) el.min = min;
					if ( el.max !== max ) el.max = max;
					if ( el.step !== "1" ) el.step = "1";
				}
				if ( el.getAttribute( "aria-label" ) !== control.label ) el.setAttribute( "aria-label", control.label );
				// A locked text edit is read-only, never disabled: the browser takes
				// focus away from a disabled element and does not give it back, so a
				// lock that lasted one network round trip left the edit unfocused.
				const textEdit = el instanceof HTMLInputElement &&
					(control.kind === "text" || control.kind === "password");
				if ( textEdit ) {
					if ( el.readOnly !== !!control.disabled ) el.readOnly = !!control.disabled;
					if ( el.disabled ) el.disabled = false;
					if ( (el.getAttribute( "aria-disabled" ) === "true") !== !!control.disabled ) {
						el.setAttribute( "aria-disabled", String( !!control.disabled ) );
					}
				} else if (
					(el instanceof HTMLInputElement || el instanceof HTMLButtonElement) &&
					el.disabled !== !!control.disabled
				) el.disabled = !!control.disabled;
				if ( control.kind === "button" ) {
					if ( el.textContent !== control.label ) el.textContent = control.label;
					if ( control.selected !== undefined ) {
						const selected = String( control.selected );
						if ( el.getAttribute( "aria-pressed" ) !== selected ) {
							el.setAttribute( "aria-pressed", selected );
						}
					} else if ( el.hasAttribute( "aria-pressed" ) ) el.removeAttribute( "aria-pressed" );
				} else if ( el instanceof HTMLInputElement && el.value !== control.value && !composing ) {
					el.value = control.value ?? "";
				}
				const [x, y, w, h] = control.rect,
					left = box.left + x + "px",
					top = box.top + y + "px",
					width = w + "px",
					height = h + "px";
				if ( el.style.left !== left ) el.style.left = left;
				if ( el.style.top !== top ) el.style.top = top;
				if ( el.style.width !== width ) el.style.width = width;
				if ( el.style.height !== height ) el.style.height = height;
				slot.value = control;
			}
			const request = state.focusRequest;
			if ( request && request.revision !== focusRevision ) {
				if ( request.id === null ) {
					focusRevision = request.revision;
					const active = document.activeElement;
					if ( active instanceof HTMLElement && root.contains( active ) ) active.blur();
					canvas.focus( { preventScroll: true } );
					return;
				}
				const slot = controls.get( request.id );
				if ( slot && !slot.value.disabled && slot.element instanceof HTMLInputElement ) {
					focusRevision = request.revision;
					slot.element.focus( { preventScroll: true } );
					slot.element.setSelectionRange( request.anchor ?? request.caret, request.caret );
					edit( slot.element, request.id );
				}
			}
		},
		dispose() {
			if ( lifetime.signal.aborted ) return;
			drag = null;
			composing = false;
			lifetime.abort();
			root.remove();
			controls.clear();
		}
	};
}
