/*
===========================================================================
workbench.ts - human navigation, local evidence ledger and viewport lifetime

The game iframe owns all production state. This shell only chooses scenarios,
captures observations and exports evidence; it never files or sends reports.
===========================================================================
*/
import { journeys, SCENARIO_VERSION } from "./scenarios";
import { uiPanels } from "../../src/engine/foundation/ui/panels";
import { coverageRows } from "./coverage-ledger";
import { prototypeScope } from "./prototype-scope";

import type { Catalog, Resource } from "./catalog-types";
import { fetchCatalog } from "./catalog-client.mjs";

interface Review {
	item: string;
	context: string;
	fingerprint: string;
	identity?: Catalog["identity"];
	browser?: string;
	time: string;
	version: number;
	axes: Record<string, string>;
	notes: string;
	status: string;
	evidence: unknown;
}
interface Draft {
	context: string;
	fingerprint: string;
	axes: Record<string, string>;
	notes: string;
}
interface Report {
	id: string;
	ready: boolean;
	entry: {
		requested: string;
		frames: number;
		busy: boolean;
		addedControls: string[];
		evidence: { admitted: boolean; missing: string[]; };
	} | null;
	coverage: { source: string; admitted: boolean; unsupported?: string; } | null;
	error: string | null;
	clock: number;
	stats: { pending: number; failed: unknown[]; panel: string; windowReady: boolean; };
	viewport: { width: number; height: number; dpr: number; };
	controls: unknown[];
	state: unknown;
	trace: { kind: string; detail: unknown; }[];
}

const STORE = "opensro.ui-qa.reviews.v1";
const DRAFT_STORE = "opensro.ui-qa.drafts.v1";
const axes = [ "visual", "interaction", "integration" ];
let catalog: Catalog, selection = "hud-tools", mode = "journeys", report: Report | null = null;
let ledger: Review[] = [];
const drafts = new Map<string, Draft>();
// Category navigation owns its filters and scroll positions; recipes never replace it.
const category = {
	id: "",
	filters: new Map<string, string>(),
	positions: new Map<string, number>(),
	last: new Map<string, string>(),
	blocked: new Map<string, string>( prototypeScope.A.remaining.map( row => [ row.id, row.blocker ] ) ),
	saving: false,
	loadedContext: "",
	loadedFields: ""
};

/*
================
element
================
*/
function element<T extends HTMLElement = HTMLElement>( id: string ): T {
	const value = document.getElementById( id );
	if ( !value ) throw Error( `Missing workbench element: ${id}` );
	return value as T;
}

const frame = element<HTMLIFrameElement>( "viewport-frame" );
const viewport = element<HTMLSelectElement>( "viewport" );
const background = element<HTMLSelectElement>( "backdrop" );

/*
================
contextKey
================
*/
function contextKey( item = selection ) {
	return item.startsWith( "resource:" ) || item.startsWith( "panel:" ) ?
		item :
		`${item}|${viewport.value}|${background.value}|dpr:${devicePixelRatio}|v${SCENARIO_VERSION}`;
}

/*
================
categoryRows
================
*/
function categoryRows() {
	return mode === "coverage" ? coverageRows().find( row => row.id === category.id ) : undefined;
}

/*
================
categoryQueue

User filtering defines the visible sequence. Never omit a blocked or reviewed row.
================
*/
function categoryQueue() {
	const filter = (category.filters.get( category.id ) ?? "").toLowerCase();
	return categoryRows()?.states.filter( row => `${row.title} ${row.id}`.toLowerCase().includes( filter ) ) ?? [];
}

/*
================
currentReview
================
*/
function currentReview( id: string ) {
	return [ ...ledger ].reverse().find( row => row.context === contextKey( id ) );
}

/*
================
categoryStatus
================
*/
function categoryStatus( id: string ) {
	const draft = drafts.get( contextKey( id ) ), saved = currentReview( id );
	const state = draft ?
		"Draft, not saved as a review" :
		saved && saved.fingerprint !== catalog.identity.fingerprint ?
		"Needs recheck" :
		saved?.status === "reviewed" ?
		"Saved judgment" :
		"Unreviewed";
	return (category.blocked.has( id ) ? "Blocked · " : "") + state;
}

/*
================
reviewFields
================
*/
function reviewFields() {
	return {
		axes: Object.fromEntries( axes.map( axis => [ axis, element<HTMLSelectElement>( axis ).value ] ) ),
		notes: element<HTMLTextAreaElement>( "notes" ).value
	};
}

/*
================
preserveDraft
================
*/
function preserveDraft() {
	if ( category.loadedContext && JSON.stringify( reviewFields() ) !== category.loadedFields ) saveDraft();
}

/*
================
flowControls
================
*/
function flowControls() {
	const group = categoryRows(), queue = categoryQueue(), at = queue.findIndex( row => row.id === selection );
	const inCategory = !!group?.states.some( row => row.id === selection );
	element( "review-flow" ).hidden = !inCategory;
	element( "save-next" ).hidden = !inCategory;
	if ( group ) {
		const saved = group.states.filter( row => {
			const review = currentReview( row.id );
			return review?.fingerprint === catalog.identity.fingerprint && review.status === "reviewed";
		} ).length;
		const draftCount = group.states.filter( row => drafts.has( contextKey( row.id ) ) ).length;
		element( "category-name" ).textContent = group.title;
		element( "category-progress" ).textContent =
			`${saved} / ${group.states.length} with saved judgments · ${draftCount} ${
				draftCount === 1 ? "draft" : "drafts"
			}. Current viewport and backdrop only.`;
		element( "flow-category" ).textContent = group.title;
		element( "flow-position" ).textContent = at < 0 ?
			"Current recipe is outside this filter." :
			`Recipe ${at + 1} of ${queue.length}${
				queue.length !== group.states.length ? ` matching filter (${group.states.length} in category)` : ""
			}`;
	}
	element<HTMLButtonElement>( "previous-recipe" ).disabled = category.saving || at <= 0;
	element<HTMLButtonElement>( "next-recipe" ).disabled = category.saving || !queue.length || at === queue.length - 1;
	element( "next-recipe" ).textContent = at < 0 ? "Next match" : "Next";
	element( "save-next" ).textContent = at === queue.length - 1 ? "Save & finish list" : "Save & Next";
	const canSave = !journeys.some( row => row.id === selection ) || !!report && (report.ready || !!report.error);
	element<HTMLButtonElement>( "save-review" ).disabled = category.saving || !canSave;
	element<HTMLButtonElement>( "save-next" ).disabled = category.saving || !canSave || at < 0;
}

/*
================
stepRecipe
================
*/
function stepRecipe( direction: number ) {
	if ( category.saving ) return;
	const queue = categoryQueue(), at = queue.findIndex( row => row.id === selection );
	const next = queue[at + direction];
	if ( !next ) return;
	select( next.id );
	element( "review-flow" ).scrollIntoView( { block: "start" } );
	const button = element( "navigation" ).querySelector<HTMLElement>( `[data-journey-id="${next.id}"]` );
	if ( button ) {
		const nav = element( "navigation" ), bounds = nav.getBoundingClientRect(), row = button.getBoundingClientRect();
		if ( row.bottom > bounds.bottom ) nav.scrollTop += row.bottom - bounds.bottom;
		else if ( row.top < bounds.top ) nav.scrollTop -= bounds.top - row.top;
	}
}

/*
================
saveAndNext

Only advance after persistence succeeds, and never navigate away from newer edits.
================
*/
async function saveAndNext() {
	const context = contextKey(), values = JSON.stringify( reviewFields() );
	if ( !await saveReview() || context !== contextKey() ) return;
	if ( values !== JSON.stringify( reviewFields() ) ) {
		element( "save-status" ).textContent =
			"Snapshot saved; newer edits remain a draft. Save again before continuing.";
		return;
	}
	const queue = categoryQueue(), at = queue.findIndex( row => row.id === selection );
	if ( at >= 0 && at < queue.length - 1 ) stepRecipe( 1 );
	else {
		const message =
			"Saved the final recipe in this list. Other unreviewed recipes remain unreviewed; nothing was skipped or marked automatically.";
		element( "flow-message" ).textContent = message;
		element( "save-status" ).textContent = message;
	}
}

/*
================
reviewLabel
================
*/
function reviewLabel( item: string, fingerprint = catalog.identity.fingerprint ) {
	// Re-review supersedes an old approval for that same context; keep history
	// in the export without making a successfully rechecked card stale forever.
	const latest = new Map<string, Review>();
	for ( const row of ledger ) if ( row.item === item ) latest.set( row.context, row );
	const reviews = [ ...latest.values() ];
	if ( !reviews.length ) return "Unreviewed";
	if ( reviews.some( row => row.fingerprint !== fingerprint ) ) return "Needs recheck";
	return reviews.some( row => row.status === "reviewed" ) ? "Reviewed checkpoint(s)" : "Unreviewed";
}

/*
================
resourceJourneys

Only named resource relationships are mapped. Family prefixes are not proof
that a scenario exercises an election, service or dynamic child.
================
*/
function resourceJourneys( name: string ) {
	const mapping: Record<string, string> = {
		ifguild: "guild-roster",
		ifguildgrantname: "guild-roster",
		ifguildgrantpower: "guild-roster",
		ifguildmasterleave: "guild-transfer",
		ifguildmasterleaveslot: "guild-transfer",
		ifguildnotifycontents: "guild-notice",
		ifstall: "stall-owner",
		ifstallslot: "stall-owner",
		ifchatmodule: "stall-owner",
		ifunderbar: "hud-tools",
		ifminimap: "hud-compact",
		ifcos: "hud-companions"
	};
	return [
		...new Set( [
			...(mapping[name] ? [ mapping[name]! ] : []),
			...journeys.filter( row => row.resources?.includes( name ) ).map( row => row.id )
		] )
	];
}

/*
================
navigation
================
*/
function navigation() {
	const filter = element<HTMLInputElement>( "search" ).value.toLowerCase();
	const group = categoryRows();
	element( "inventory-search" ).hidden = !!group;
	element( "category-context" ).hidden = !group;
	const rows = group ?
		categoryQueue().map( row => ({ id: row.id, title: row.title, detail: categoryStatus( row.id ) }) ) :
		mode === "coverage" ?
		coverageRows().map( row => ({
			id: "coverage:" + row.id,
			title: row.title,
			detail: `${row.states.length} registered recipes; remaining transitions disclosed`
		}) ) :
		mode === "journeys" ?
		journeys.map( journey => ({
			id: journey.id,
			title: journey.title,
			detail: `${journey.family} · ${journey.family === "Panels" ? "Entry probe" : "Partial coverage"} · ${
				reviewLabel( journey.id )
			}`
		}) ) :
		[
			...uiPanels().map( name => ({
				id: `panel:${name}`,
				title: name,
				detail: "Panel · production entry probe available"
			}) ),
			...catalog.resources.map( row => ({
				id: `resource:${row.name}`,
				title: row.name,
				detail: `${
					resourceJourneys( row.name ).length ?
						"Related recipe available; coverage unverified" :
						row.references.length ?
						"Referenced · no scenario" :
						"Unmapped"
				} · ${reviewLabel( `resource:${row.name}` )}`
			}) )
		];
	const nav = element( "navigation" );
	const position = category.positions.get( group ? "category:" + group.id : mode ) ?? 0;
	nav.replaceChildren();
	for ( const row of rows.filter( row => group || `${row.title} ${row.detail}`.toLowerCase().includes( filter ) ) ) {
		const button = document.createElement( "button" );
		button.dataset.journeyId = row.id;
		button.textContent = row.title;
		button.classList.toggle( "selected", row.id === selection );
		if ( row.id === selection ) button.setAttribute( "aria-current", "step" );
		button.disabled = category.saving;
		button.classList.toggle( "blocked", category.blocked.has( row.id ) );
		const sub = document.createElement( "small" );
		sub.textContent = row.detail;
		button.append( sub );
		button.onclick = () => select( row.id );
		nav.append( button );
	}
	nav.scrollTop = position;
	element( "counts" ).textContent = group ?
		`${rows.length} recipes in this list. No automatic skipping.` :
		mode === "journeys" ?
		[ ...new Set( journeys.map( row => row.family ) ) ].map( family =>
			`${family}: ${journeys.filter( row => row.family === family ).length}`
		).join( " · " ) :
		`${catalog.resources.length} resources · ${uiPanels().length} panels · references are not coverage`;
	flowControls();
}

/*
================
reset

A new document resets private UI closures, preferences, async owners and DOM.
Viewport/background changes are new review contexts and deliberately reset.
================
*/
function reset() {
	if ( !journeys.some( row => row.id === selection ) ) return;
	preserveDraft();
	const [width, height] = viewport.value.split( "x" );
	frame.width = width!;
	frame.height = height!;
	report = null;
	element( "runtime-status" ).textContent = "Loading production assets…";
	element<HTMLButtonElement>( "save-review" ).disabled = true;
	frame.src = `./frame.html?scenario=${
		encodeURIComponent( selection )
	}&backdrop=${background.value}&reset=${Date.now()}&diagnostics`;
	loadReview();
	flowControls();
}

/*
================
showResource
================
*/
function showResource( resource: Resource | undefined, panel?: string ) {
	const host = element( "resource" );
	host.replaceChildren();
	const mapped = resource ? resourceJourneys( resource.name ) : panel ? [ `surface:${panel}` ] : [];
	const text = document.createElement( "p" );
	text.textContent = mapped.length ?
		"A related journey is available. It does not cover every authored control or dynamic state." :
		"No supported scenario in this milestone. Keep this item in the review queue; absence here does not establish a product defect.";
	host.append( text );
	for ( const id of mapped ) {
		const button = document.createElement( "button" );
		button.textContent = "Open related journey: " + (journeys.find( row => row.id === id )?.title ?? id);
		button.onclick = () => select( id );
		host.append( button );
	}
	if ( !resource ) return;
	if ( resource.url ) {
		const link = document.createElement( "a" );
		link.href = resource.url;
		link.target = "_blank";
		link.rel = "noopener";
		link.textContent = "Open published resource JSON";
		host.append( document.createElement( "p" ), link );
	}
	const pre = document.createElement( "pre" );
	pre.textContent = JSON.stringify(
		{
			sourceHash: resource.nativeHash,
			publishedHash: resource.publishedHash,
			literalReferences: resource.references,
			authoredSections: resource.sections
		},
		null,
		2
	);
	host.append( pre );
}

/*
================
select
================
*/
function select( id: string ) {
	preserveDraft();
	if ( id.startsWith( "coverage:" ) ) {
		category.id = id.slice( 9 );
		element<HTMLInputElement>( "category-search" ).value = category.filters.get( category.id ) ?? "";
	} else if ( categoryRows()?.states.some( row => row.id === id ) ) category.last.set( category.id, id );
	selection = id;
	// Do not reinterpret the previous recipe's fields as a draft for this one.
	category.loadedContext = "";
	element( "flow-message" ).textContent = "";
	const journey = journeys.find( row => row.id === id );
	element( "runner" ).hidden = !journey;
	element( "resource" ).hidden = !!journey;
	element( "family" ).textContent = journey ?
		`${journey.family} / SYNTHETIC STATE · VERSION ${SCENARIO_VERSION}` :
		"INVENTORY / AUTHORED REFERENCE";
	element( "title" ).textContent = journey?.title ?? id.slice( id.indexOf( ":" ) + 1 );
	element( "description" ).textContent = journey?.description ??
		"Resource and entry-point discovery. Raw metadata is not a native runtime screenshot.";
	element( "limits" ).textContent = journey?.limits ?? catalog.limitations;
	if ( journey ) {
		element( "instructions" ).textContent = journey.instructions;
		const actions = element( "actions" );
		actions.replaceChildren();
		for ( const [name, label] of journey.actions ) {
			const button = document.createElement( "button" );
			button.textContent = label;
			button.dataset.action = name;
			button.onclick = () => send( name );
			actions.append( button );
		}
		reset();
	} else {
		frame.src = "about:blank";
		report = null;
		if ( id.startsWith( "coverage:" ) ) showCoverage( id.slice( 9 ) );
		else {showResource(
				catalog.resources.find( row => `resource:${row.name}` === id ),
				id.startsWith( "panel:" ) ? id.slice( 6 ) : undefined
			);}
		element<HTMLButtonElement>( "save-review" ).disabled = false;
		loadReview();
	}
	navigation();
}

/*
================
showCoverage
================
*/
function showCoverage( id: string ) {
	const row = coverageRows().find( value => value.id === id );
	const host = element( "resource" );
	host.replaceChildren();
	if ( !row ) return;
	element( "title" ).textContent = row.title;
	element( "description" ).textContent = "Source: " + row.source;
	const resume = category.last.get( id ), first = categoryQueue()[0]?.id;
	const start = document.createElement( "button" );
	start.className = "primary";
	start.textContent = resume ? "Continue category review" : "Start category review";
	start.disabled = !resume && !first;
	start.onclick = () => select( resume ?? first! );
	host.append( start );
	element( "limits" ).textContent = "A: " +
		(row.prototype.A.map( item => item.blocker ).join( " " ) || "No outstanding unique layout in this inventory.") +
		" B: No outstanding essential local operation. C (optional, outside completion): " + row.remaining;
	const checklist = document.createElement( "details" ),
		summary = document.createElement( "summary" ),
		contents = document.createElement( "pre" );
	summary.textContent = "Finite prototype checklist: A / B / C";
	contents.textContent = JSON.stringify( prototypeScope, null, 2 );
	checklist.append( summary, contents );
	host.append( checklist );
	for ( const state of row.states ) {
		const button = document.createElement( "button" );
		button.textContent = state.title + " — " + reviewLabel( state.id );
		button.onclick = () => select( state.id );
		const detail = document.createElement( "p" );
		detail.textContent = `${state.id} | ${state.source} | ${state.verification}`;
		host.append( button, detail );
	}
}

/*
================
send
================
*/
function send( action: string ) {
	frame.contentWindow?.postMessage( { type: "uiqa-action", action }, location.origin );
}

/*
================
loadReview
================
*/
function loadReview() {
	const row = [ ...ledger ].reverse().find( review => review.context === contextKey() );
	const draft = drafts.get( contextKey() );
	for ( const axis of axes ) {
		element<HTMLSelectElement>( axis ).value = draft?.axes[axis] ?? row?.axes[axis] ?? "unreviewed";
	}
	element<HTMLTextAreaElement>( "notes" ).value = draft?.notes ?? row?.notes ?? "";
	element( "review-status" ).textContent = draft ?
		`Draft — not reviewed${
			draft.fingerprint !== catalog.identity.fingerprint ? "; source changed, recheck before saving" : ""
		}` :
		row ?
		row.fingerprint === catalog.identity.fingerprint ?
			`${row.status} · ${new Date( row.time ).toLocaleString()}` :
			"Needs recheck — source/assets changed since this record" :
		"Unreviewed in this viewport and background";
	element( "save-status" ).textContent = "";
	category.loadedContext = contextKey();
	category.loadedFields = JSON.stringify( reviewFields() );
}

/*
================
saveDraft

Preserve incomplete notes across navigation and reset without granting review.
================
*/
function saveDraft() {
	const key = category.loadedContext || contextKey();
	drafts.set( key, {
		context: key,
		fingerprint: catalog.identity.fingerprint,
		axes: Object.fromEntries( axes.map( axis => [ axis, element<HTMLSelectElement>( axis ).value ] ) ),
		notes: element<HTMLTextAreaElement>( "notes" ).value
	} );
	try {
		localStorage.setItem( DRAFT_STORE, JSON.stringify( [ ...drafts.values() ] ) );
		element( "review-status" ).textContent = "Draft saved locally — not reviewed";
	} catch ( error ) {
		element( "save-status" ).textContent = `Draft is in memory only: ${String( error )}`;
	}
	navigation();
}

/*
================
saveReview

Refresh identity before writing: never attach fresh approval to stale modules.
Storage errors are visible and leave the in-memory ledger exportable.
================
*/
async function saveReview() {
	if ( category.saving ) return false;
	preserveDraft();
	category.saving = true;
	flowControls();
	// Capture the reviewed document and fields before asynchronous identity I/O.
	const values = Object.fromEntries( axes.map( axis => [ axis, element<HTMLSelectElement>( axis ).value ] ) );
	const entry: Review = {
		item: selection,
		context: contextKey(),
		fingerprint: catalog.identity.fingerprint,
		identity: structuredClone( catalog.identity ),
		browser: navigator.userAgent,
		time: new Date().toISOString(),
		version: SCENARIO_VERSION,
		axes: values,
		notes: element<HTMLTextAreaElement>( "notes" ).value,
		status: Object.values( values ).every( value => value === "unreviewed" ) ? "unreviewed" : "reviewed",
		evidence: report ? structuredClone( report ) : { resource: selection, availability: "not exercised" }
	};
	try {
		const current = await fetchCatalog();
		if ( current.identity.fingerprint !== catalog.identity.fingerprint ) {
			element( "save-status" ).textContent =
				"Source or assets changed. Reload the workbench before recording this review.";
			return false;
		}
		const nextLedger = [ ...ledger, entry ];
		localStorage.setItem( STORE, JSON.stringify( nextLedger ) );
		ledger = nextLedger;
		const draft = drafts.get( entry.context );
		if ( draft?.notes === entry.notes && JSON.stringify( draft.axes ) === JSON.stringify( entry.axes ) ) {
			drafts.delete( entry.context );
		}
		localStorage.setItem( DRAFT_STORE, JSON.stringify( [ ...drafts.values() ] ) );
		if ( contextKey() === entry.context ) loadReview();
		navigation();
		element( "save-status" ).textContent = "Saved on this browser. Export to keep a portable copy.";
		return true;
	} catch ( error ) {
		element( "save-status" ).textContent = `Could not persist review: ${
			String( error )
		}. Export is still available.`;
		return false;
	} finally {
		category.saving = false;
		navigation();
	}
}

/*
================
exportLedger

Includes unreviewed inventory and explicitly bounded coverage, not only notes.
================
*/
async function exportLedger() {
	let current: Catalog;
	try {
		current = await fetchCatalog();
	} catch ( error ) {
		element( "save-status" ).textContent = `Export could not verify current identity: ${String( error )}`;
		return;
	}
	const evidence = {
		format: "opensro-ui-qa-v1",
		exportedAt: new Date().toISOString(),
		identity: current.identity,
		roots: current.roots,
		browser: navigator.userAgent,
		devicePixelRatio,
		limitations: catalog.limitations,
		nativeReference: "Not provided; no native correctness certification",
		journeys: journeys.map( row => ({
			...row,
			version: SCENARIO_VERSION,
			availability: row.family === "Panels" ? "entry probe; state coverage unverified" : "partial",
			review: reviewLabel( row.id, current.identity.fingerprint )
		}) ),
		resources: current.resources.map( row => ({
			...row,
			relatedJourneys: resourceJourneys( row.name ),
			availability: resourceJourneys( row.name ).length ?
				"related recipe available; reachability and coverage unverified" :
				row.references.length ?
				"referenced resource; no mapped recipe" :
				"unmapped resource",
			review: reviewLabel( `resource:${row.name}`, current.identity.fingerprint )
		}) ),
		prototypeScope,
		familyCoverage: coverageRows().map( family => ({
			...family,
			states: family.states.map( state => ({
				...state,
				humanReview: reviewLabel( state.id, current.identity.fingerprint )
			}) )
		}) ),
		panels: uiPanels().map( name => ({
			name,
			availability: "entry probe; state coverage unverified",
			journey: `surface:${name}`
		}) ),
		reviews: ledger.map( row => ({
			...row,
			status: row.fingerprint === current.identity.fingerprint ? row.status : "needs-recheck"
		}) ),
		drafts: [ ...drafts.values() ].map( draft => ({ ...draft, status: "unreviewed draft" }) )
	};
	const url = URL.createObjectURL(
		new Blob( [ JSON.stringify( evidence, null, 2 ) ], { type: "application/json" } )
	);
	const link = document.createElement( "a" );
	link.href = url;
	link.download = "opensro-ui-review.json";
	link.click();
	setTimeout( () => URL.revokeObjectURL( url ), 1000 );
}

/*
================
start
================
*/
async function start() {
	catalog = await fetchCatalog();
	try {
		ledger = JSON.parse( localStorage.getItem( STORE ) ?? "[]" );
	} catch {
		ledger = [];
	}
	if ( !Array.isArray( ledger ) ) ledger = [];
	ledger = ledger.filter( row =>
		row && typeof row.item === "string" && typeof row.context === "string" && row.axes &&
		typeof row.notes === "string"
	);
	try {
		const stored: Draft[] = JSON.parse( localStorage.getItem( DRAFT_STORE ) ?? "[]" );
		if ( Array.isArray( stored ) ) {
			for ( const draft of stored ) {
				if (
					draft && typeof draft.context === "string" && draft.axes && typeof draft.notes === "string"
				) drafts.set( draft.context, draft );
			}
		}
	} catch { /* A corrupt draft cannot prevent opening the review inventory. */ }
	element( "identity" ).textContent = `${catalog.identity.codeSha.slice( 0, 12 )}${
		catalog.identity.dirty ? " + local changes" : ""
	} · assets ${catalog.identity.assetHash.slice( 0, 10 )}`;
	for ( const axis of axes ) {
		const select = element<HTMLSelectElement>( axis );
		for ( const value of [ "unreviewed", "acceptable", "discrepancy", "uncertain", "unsupported" ] ) {
			select.add( new Option( value, value ) );
		}
		select.onchange = saveDraft;
	}
	element( "notes" ).oninput = saveDraft;
	element( "search" ).oninput = navigation;
	element( "navigation" ).onscroll = () =>
		category.positions.set( categoryRows() ? "category:" + category.id : mode, element( "navigation" ).scrollTop );
	element( "category-search" ).oninput = () => {
		category.filters.set( category.id, element<HTMLInputElement>( "category-search" ).value );
		category.positions.set( "category:" + category.id, 0 );
		navigation();
	};
	element( "all-categories" ).onclick = () => {
		category.id = "";
		navigation();
	};
	element( "category-overview" ).onclick = () => select( "coverage:" + category.id );
	element( "previous-recipe" ).onclick = () => stepRecipe( -1 );
	element( "next-recipe" ).onclick = () => stepRecipe( 1 );
	element( "save-next" ).onclick = saveAndNext;
	for ( const tab of [ "journeys", "inventory", "coverage" ] ) {
		element( `${tab}-tab` ).onclick = () => {
			mode = tab;
			for ( const name of [ "journeys", "inventory", "coverage" ] ) {
				element( `${name}-tab` ).setAttribute( "aria-pressed", String( name === mode ) );
			}
			navigation();
		};
	}
	element( "reset" ).onclick = reset;
	viewport.onchange = reset;
	background.onchange = reset;
	element( "advance" ).onclick = () => send( "advance" );
	element( "save-review" ).onclick = saveReview;
	element( "export" ).onclick = exportLedger;
	window.addEventListener( "message", message => {
		if (
			message.origin !== location.origin || message.source !== frame.contentWindow ||
			message.data?.type !== "uiqa-status"
		) return;
		const next = message.data.report as Report;
		if ( next.id !== selection ) return;
		report = next;
		if ( next.error && category.blocked.get( selection ) !== next.error ) {
			category.blocked.set( selection, next.error );
			navigation();
		}
		const limitation = [ ...next.trace ].reverse().find( row =>
			/unsupported|unavailable|coverage limit/.test( row.kind )
		);
		const admission = next.coverage ?
			(next.coverage.admitted && !next.entry?.busy ?
				"Production surface admitted; further state coverage remains partial. " :
				(next.coverage.unsupported ?
					`Unsupported: ${next.coverage.unsupported} ` :
					"Waiting for recipe evidence; this surface is not yet verified. ")) :
			selection.startsWith( "surface:" ) || selection.startsWith( "service:" ) ?
			(selection.startsWith( "service:" ) || next.stats.panel === selection.slice( 8 )) && next.entry &&
				!next.entry.busy && next.entry.frames >= 3 &&
				next.entry.evidence.admitted && next.stats.windowReady && next.stats.pending === 0 ?
				"Requested presentation mounted. Visible menu entry, further states and server behavior remain unverified. " :
				`Requested surface is unverified. Missing target controls: ${
					next.entry?.evidence.missing.join( ", " ) ?? "waiting"
				}. ` :
			"";
		element( "activity" ).textContent = admission + (limitation ?
			`${limitation.kind}: ${
				typeof limitation.detail === "string" ? limitation.detail : JSON.stringify( limitation.detail )
			}` :
			"");
		element( "runtime-status" ).textContent = next.error ?
			next.error :
			next.ready ?
			`${next.controls.length} controls · ${next.stats.pending} pending · ${next.stats.failed.length} failed · clock ${next.clock}ms` :
			"Loading production assets…";
		flowControls();
		element( "trace" ).textContent = JSON.stringify(
			{ state: next.state, stats: next.stats, trace: next.trace },
			null,
			2
		);
	} );
	select( selection );
}

start().catch( error => {
	element( "title" ).textContent = "Workbench could not start";
	element( "description" ).textContent = String( error );
} );
