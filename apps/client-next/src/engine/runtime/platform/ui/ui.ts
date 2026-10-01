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
	heldKey: ( code: string, down: boolean ) => void = () => {},
	displayScale: () => number = () => 1
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
	// Each control element and the layout this bridge last wrote to it: the
	// bridge is the only writer, so it compares numbers instead of reading the
	// element's style back (a CSSOM read and string per control per present).
	const controls = new Map<
		string,
		{
			element: HTMLInputElement | HTMLButtonElement | HTMLDivElement;
			value: UiControl;
			order: number;
			box: [number, number, number, number];
		}
	>();
	let composing = false;
	let drag: { id: string; pointer: number; x: number; y: number; moved?: boolean; } | null = null, focusRevision = -1;
	let suppressClick: string | null = null;
	// Abandoning a drag or a carry is reported as drag-cancel, never as
	// press null: press null is the visual release that every key-up and
	// control boundary crossing also sends, and a carried item must survive
	// those.
	// Click-carry: a click on a carry control lifts its item onto the cursor;
	// pointer moves report as that control's drag, and the next left press
	// reports its drag-end at the press point and is consumed whole, so it
	// neither re-presses a control nor reaches the world. A press back on the
	// source puts the item down and passes through (its double-click still
	// fires); right press or Escape cancels. swallowClick eats the consumed
	// press's click; suppressCarry stops a put-back click lifting it again.
	let carry: { id: string; x: number; y: number; } | null = null,
		swallowClick = false,
		suppressCarry: string | null = null;
	/*
	================
	uiPoint

	A pointer position in UI pixels. Drag deltas and drop points are UI
	coordinates: CSS client pixels relative to the canvas, divided by the
	chosen screen size's display scale (the inverse of the control layout).
	================
	*/
	function uiPoint( event: { clientX: number; clientY: number; } ): [number, number] {
		const box = canvas.getBoundingClientRect(), scale = displayScale();
		return [ (event.clientX - box.left) / scale, (event.clientY - box.top) / scale ];
	}
	let rightPressed: { element: Element; pointer: number; } | null = null;
	window.addEventListener( "pointermove", event => {
		if ( !drag || event.pointerId !== drag.pointer ) return;
		const current = controls.get( drag.id )?.value;
		if ( !current?.draggable || current.disabled ) {
			emit( { kind: "drag-cancel", id: drag.id } );
			drag = null;
			emit( { kind: "press", id: null } );
			return;
		}
		const [x, y] = uiPoint( event ), dx = x - drag.x, dy = y - drag.y;
		drag = { ...drag, x, y, moved: drag.moved || dx !== 0 || dy !== 0 };
		emit( { kind: "drag", id: drag.id, dx, dy } );
	}, { signal: lifetime.signal } );
	window.addEventListener( "pointerup", event => {
		if ( event.button !== 0 ) return;
		if ( drag && event.pointerId !== drag.pointer ) return;
		const completed = drag;
		drag = null;
		if ( completed?.moved ) {
			suppressClick = completed.id;
			const [x, y] = uiPoint( event );
			emit( { kind: "drag-end", id: completed.id, x, y } );
		}
	}, { signal: lifetime.signal } );
	window.addEventListener( "pointercancel", () => {
		if ( drag ) emit( { kind: "drag-cancel", id: drag.id } );
		drag = null;
		suppressClick = null;
		emit( { kind: "press", id: null } );
	}, { signal: lifetime.signal } );
	window.addEventListener( "blur", () => {
		if ( drag ) emit( { kind: "drag-cancel", id: drag.id } );
		drag = null;
		cancelCarry();
	}, { signal: lifetime.signal } );
	/*
	================
	edit

	Publish browser-owned text and selection together, including IME state.
	================
	*/
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
		if ( event.button === 0 && (!drag || event.pointerId === drag.pointer) ) emit( { kind: "press", id: null } );
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
	/*
	================
	current

	Delegated events resolve current state, never a descriptor from creation.
	================
	*/
	function current( target: EventTarget | null ) {
		if ( !(target instanceof Element) ) return;
		const el = target.closest<HTMLElement>( "[data-ui-id]" );
		if ( !el || !root.contains( el ) ) return;
		const slot = controls.get( el.dataset.uiId! );
		return slot?.element === el ? slot : undefined;
	}
	// Drag regions prevent the browser's default pointer-down focus transfer.
	// Retire the previous control's focus first, including a press on the world,
	// so a button cannot remain highlighted after the player starts a new action.
	window.addEventListener( "pointerdown", event => {
		if ( event.button !== 0 ) return;
		const active = document.activeElement;
		if ( !(active instanceof HTMLElement) || !root.contains( active ) ) return;
		const target = current( event.target );
		if ( target?.element === active ) return;
		active.blur();
		if ( event.target === canvas || target?.value.kind === "region" ) canvas.focus( { preventScroll: true } );
	}, { capture: true, signal: lifetime.signal } );
	root.addEventListener( "pointerdown", event => {
		if ( event.button !== 2 ) return;
		const slot = current( event.target );
		rightPressed = slot?.value.rightActivate && !slot.value.disabled ?
			{ element: slot.element, pointer: event.pointerId } :
			null;
	}, { signal: lifetime.signal } );
	// Browsers omit pointerdown/up for intermediate buttons in a mouse chord.
	// Keep that native cancel gesture reachable while a left drag owns capture.
	root.addEventListener( "mousedown", event => {
		if ( event.button !== 2 || !drag || rightPressed ) return;
		const slot = current( event.target );
		if ( slot?.value.rightActivate && !slot.value.disabled ) {
			rightPressed = { element: slot.element, pointer: drag.pointer };
		}
	}, { signal: lifetime.signal } );
	/*
	================
	finishRightPress

	Release must hit the same live control even while another button captures
	the pointer. Mouseup is the chord fallback; clearing the press deduplicates it.
	================
	*/
	function finishRightPress( event: MouseEvent ) {
		if ( event.button !== 2 || !rightPressed ) return;
		if ( event instanceof PointerEvent && event.pointerId !== rightPressed.pointer ) return;
		const armed = rightPressed;
		rightPressed = null;
		const slot = current( document.elementFromPoint( event.clientX, event.clientY ) );
		// 5650A0 uses the pressed control and admits release only inside it.
		if ( slot?.element === armed.element && slot.value.rightActivate && !slot.value.disabled ) {
			emit( {
				kind: "right-activate",
				id: slot.value.id,
				shift: event.shiftKey,
				ctrl: event.ctrlKey,
				alt: event.altKey
			} );
			if ( drag ) {
				const element = controls.get( drag.id )?.element;
				if ( element?.hasPointerCapture( drag.pointer ) ) element.releasePointerCapture( drag.pointer );
				suppressClick = drag.id;
				emit( { kind: "drag-cancel", id: drag.id } );
				drag = null;
				emit( { kind: "press", id: null } );
			}
		}
	}
	window.addEventListener( "pointerup", finishRightPress, { signal: lifetime.signal } );
	window.addEventListener( "mouseup", finishRightPress, { signal: lifetime.signal } );
	root.addEventListener( "pointermove", event => {
		// A captured left press suppresses compatibility mouse events. Pointer
		// events encode subsequent button changes as moves with button == 2.
		if ( event.button !== 2 || !drag ) return;
		if ( event.buttons & 2 ) {
			const slot = current( event.target );
			if ( slot?.value.rightActivate && !slot.value.disabled ) {
				rightPressed = { element: slot.element, pointer: event.pointerId };
			}
		} else finishRightPress( event );
	}, { signal: lifetime.signal } );
	window.addEventListener( "pointercancel", () => {
		rightPressed = null;
	}, { signal: lifetime.signal } );
	window.addEventListener( "blur", () => {
		rightPressed = null;
	}, { signal: lifetime.signal } );
	root.addEventListener( "click", event => {
		const slot = current( event.target ), suppressed = suppressClick, putBack = suppressCarry;
		suppressClick = null;
		suppressCarry = null;
		if ( slot?.value.kind === "button" && !slot.value.disabled && slot.value.id !== suppressed ) {
			emit( {
				kind: "activate",
				id: slot.value.id,
				shift: event.shiftKey,
				ctrl: event.ctrlKey,
				alt: event.altKey
			} );
			// Keyboard activation (detail 0) never lifts an item. Nor does a
			// modifier click: CIFItemSlot_DispatchActivation (567290) turns CTRL,
			// SHIFT and ALT clicks into the quick sale/buy, the stack split and
			// the COS transfer, exclusive of the plain click. Lifting here too
			// threw the icon onto the cursor until the sale disabled its slot.
			const modified = event.ctrlKey || event.shiftKey || event.altKey;
			if (
				slot.value.carry && slot.value.draggable && !carry && event.detail > 0 && !modified &&
				putBack !== slot.value.id
			) {
				const [x, y] = uiPoint( event ), rect = slot.value.rect;
				carry = { id: slot.value.id, x, y };
				// The lifted icon starts on the cursor, not on the slot's centre.
				emit( {
					kind: "drag",
					id: carry.id,
					dx: x - (rect[0] + rect[2] / 2),
					dy: y - (rect[1] + rect[3] / 2)
				} );
			}
		}
	}, { signal: lifetime.signal } );
	/*
	================
	carrying

	The live carry, dropped silently once its control no longer carries.
	================
	*/
	function carrying() {
		const value = carry ? controls.get( carry.id )?.value : undefined;
		if ( carry && (!value?.carry || value.disabled) ) {
			emit( { kind: "drag-cancel", id: carry.id } );
			carry = null;
		}
		return carry;
	}
	/*
	================
	cancelCarry
	================
	*/
	function cancelCarry() {
		if ( !carry ) return;
		emit( { kind: "drag-cancel", id: carry.id } );
		carry = null;
	}
	window.addEventListener( "pointermove", event => {
		const live = drag ? null : carrying();
		if ( !live ) return;
		const [x, y] = uiPoint( event ), dx = x - live.x, dy = y - live.y;
		carry = { ...live, x, y };
		if ( dx !== 0 || dy !== 0 ) emit( { kind: "drag", id: live.id, dx, dy } );
	}, { signal: lifetime.signal } );
	window.addEventListener( "pointerdown", event => {
		swallowClick = false;
		const live = carrying();
		if ( !live ) return;
		if ( event.button === 0 && current( event.target )?.value.id === live.id ) {
			suppressCarry = live.id;
			cancelCarry();
			return;
		}
		event.preventDefault();
		event.stopPropagation();
		carry = null;
		swallowClick = true;
		if ( event.button !== 0 ) {
			emit( { kind: "drag-cancel", id: live.id } );
			return;
		}
		const [x, y] = uiPoint( event );
		emit( { kind: "drag-end", id: live.id, x, y } );
	}, { capture: true, signal: lifetime.signal } );
	for ( const name of [ "mousedown", "click", "dblclick", "contextmenu" ] as const ) {
		window.addEventListener( name, event => {
			if ( !swallowClick ) return;
			event.preventDefault();
			event.stopPropagation();
		}, { capture: true, signal: lifetime.signal } );
	}
	window.addEventListener( "keydown", event => {
		if ( event.code !== "Escape" || !carry ) return;
		event.preventDefault();
		event.stopPropagation();
		cancelCarry();
	}, { capture: true, signal: lifetime.signal } );
	root.addEventListener( "dblclick", event => {
		const slot = current( event.target );
		if ( slot?.value.kind === "region" ) {
			// Regions report where, in UI pixels.
			const box = canvas.getBoundingClientRect(), scale = displayScale();
			emit( {
				kind: "region-double",
				id: slot.value.id,
				x: (event.clientX - box.left) / scale,
				y: (event.clientY - box.top) / scale
			} );
			return;
		}
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
	// The last pointer position and the hover last reported. Browsers send
	// boundary events only when the pointer moves, so a control that is
	// re-enabled (an inventory slot after a pending sale), appears (a window
	// opening) or disappears under a still pointer would keep a stale hover.
	// Each publication re-tests the element under the pointer (syncHover).
	let pointerAt: [number, number] | null = null, reportedHover: string | null = null;
	/*
	================
	reportHover
	================
	*/
	function reportHover( id: string | null ) {
		reportedHover = id;
		emit( { kind: "hover", id } );
	}
	window.addEventListener( "pointermove", event => {
		pointerAt = [ event.clientX, event.clientY ];
	}, { signal: lifetime.signal, passive: true } );
	/*
	================
	syncHover

	The hover a still pointer should report after this publication.
	================
	*/
	function syncHover() {
		if ( !pointerAt || drag ) return;
		const slot = current( document.elementFromPoint( pointerAt[0], pointerAt[1] ) );
		const id = slot && !slot.value.disabled ? slot.value.id : null;
		if ( id !== reportedHover ) reportHover( id );
	}
	root.addEventListener( "pointerover", event => {
		const slot = current( event.target );
		if ( slot !== current( event.relatedTarget ) ) {
			reportHover( slot && !slot.value.disabled ? slot.value.id : null );
		}
	}, { signal: lifetime.signal } );
	root.addEventListener( "pointerout", event => {
		const next = current( event.relatedTarget );
		if ( current( event.target ) !== next ) {
			reportHover( next && !next.value.disabled ? next.value.id : null );
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
			const [x, y] = uiPoint( event );
			drag = { id: slot.value.id, pointer: event.pointerId, x, y };
		}
	}, { signal: lifetime.signal } );
	/*
	================
	retire

	Release capture and focus before removing an obsolete semantic control.
	================
	*/
	function retire( id: string ) {
		const slot = controls.get( id );
		if ( !slot ) return;
		if ( rightPressed?.element === slot.element ) rightPressed = null;
		if ( carry?.id === id ) cancelCarry();
		if ( drag?.id === id ) {
			if ( slot.element.hasPointerCapture( drag.pointer ) ) slot.element.releasePointerCapture( drag.pointer );
			emit( { kind: "drag-cancel", id } );
			drag = null;
			emit( { kind: "press", id: null } );
		}
		if ( document.activeElement === slot.element ) slot.element.blur();
		slot.element.remove();
		controls.delete( id );
	}
	return {
		/*
		================
		present

		Validate identities before reconciling the keyed DOM tree. Stable nodes
		preserve browser selection and accessibility state between GPU frames.
		================
		*/
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
			// Hit geometry this publication changes. Pointer motion over unchanged
			// controls reports hover through native pointerover/pointerout; only a
			// changed layout under a still pointer needs the hit test (which forces a
			// synchronous layout, so it no longer runs on every frame).
			let hitLayoutChanged = false;
			const wanted = new Set( state.controls.map( c => c.id ) );
			for ( const id of controls.keys() ) {
				if ( !wanted.has( id ) ) {
					retire( id );
					hitLayoutChanged = true;
				}
			}
			const box = canvas.getBoundingClientRect();
			for ( const [order, control] of state.controls.entries() ) {
				let slot = controls.get( control.id );
				if ( slot && slot.value.kind !== control.kind ) {
					retire( control.id );
					slot = undefined;
				}
				if ( !slot ) {
					hitLayoutChanged = true;
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
					slot = { element, value: control, order: NaN, box: [ NaN, NaN, NaN, NaN ] };
					controls.set( control.id, slot );
				}
				const el = slot.element;
				if ( slot.order !== order ) {
					slot.order = order;
					el.style.zIndex = String( order );
					hitLayoutChanged = true;
				}
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
				// UI pixels to CSS pixels (platform displayScale: the chosen screen size).
				const scale = displayScale(),
					[x, y, w, h] = control.rect,
					left = box.left + x * scale,
					top = box.top + y * scale,
					width = w * scale,
					height = h * scale,
					applied = slot.box;
				if ( applied[0] !== left || applied[1] !== top || applied[2] !== width || applied[3] !== height ) {
					applied[0] = left;
					applied[1] = top;
					applied[2] = width;
					applied[3] = height;
					el.style.left = left + "px";
					el.style.top = top + "px";
					el.style.width = width + "px";
					el.style.height = height + "px";
					hitLayoutChanged = true;
				}
				if ( !!slot.value.disabled !== !!control.disabled ) hitLayoutChanged = true;
				slot.value = control;
			}
			if ( hitLayoutChanged ) syncHover();
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
		/*
		================
		dispose

		Abort delegated listeners before discarding controls and their state.
		================
		*/
		dispose() {
			if ( lifetime.signal.aborted ) return;
			drag = null;
			carry = null;
			rightPressed = null;
			composing = false;
			lifetime.abort();
			root.remove();
			controls.clear();
		}
	};
}
