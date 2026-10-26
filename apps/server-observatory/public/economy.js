/*
===========================================================================

economy.js - the Economy page: one server, one range, five read-only views
and the staff seal census

Loads the GameWorld economy aggregates through this console's proxy
(/api/economy/*, server/economy.mjs) and draws them with economy-view.js.
Each section loads and fails on its own, so a missing snapshot or an older
GameWorld leaves the rest of the page readable. Text from the server is
always set as text; charts are SVG strings whose labels are escaped.
The Seal census tab is the page's one action: Run census asks the shard
to count its seal items over a fresh store copy (it changes nothing).

===========================================================================
*/
import { postJSON, requestJSON } from "./request.js";
import {
	alignSeries,
	barChart,
	CENSUS_LOCATION_LABELS,
	censusRows,
	compactGold,
	countText,
	flagLabel,
	flowRows,
	groupDigits,
	killStatRows,
	lineChart,
	plusBars,
	RANGES,
	rangeWindow,
	rateText,
	reasonLabel,
	recoveryLink,
	seriesTotal,
	share,
	sparkline,
	toBig
} from "./economy-view.js";

const TABS = [ "overview", "flows", "trade", "rates", "census", "flags" ];
const RANGE_KEY = "sro-observatory-economy-range";
const status = byID( "status" );
let generation = 0, meta = null, loaded = null, paintedWidth = 0;
// The newest census answer for the tab: censusReport, or censusFailed's
// text when the shard refused (off, none run yet, one already running).
let censusReport = null, censusFailed = "";

/*
================
byID
================
*/
function byID( id ) {
	return document.getElementById( "economy-" + id ) ?? document.getElementById( id );
}

/*
================
element
================
*/
function element( tag, text = "", className = "" ) {
	const node = document.createElement( tag );
	node.textContent = text;
	if ( className ) node.className = className;
	return node;
}

/*
================
table

headings are column names; rows are arrays of strings or nodes. Numeric
columns (by index in numeric) align right.
================
*/
function table( headings, rows, numeric = [] ) {
	const node = document.createElement( "table" ), head = node.createTHead().insertRow();
	// Two columns stay rows on a phone; wider tables become cards titled by
	// their first column (economy.css).
	node.className = headings.length === 2 ? "economy-pairs" : "economy-cards";
	headings.forEach( ( heading, i ) => {
		const cell = element( "th", heading );
		if ( numeric.includes( i ) ) cell.className = "num";
		head.append( cell );
	} );
	const body = node.createTBody();
	for ( const row of rows ) {
		const line = body.insertRow();
		row.forEach( ( value, i ) => {
			const cell = line.insertCell();
			cell.dataset.label = headings[i];
			if ( numeric.includes( i ) ) cell.className = "num";
			if ( value instanceof Node ) cell.append( value );
			else cell.textContent = value;
		} );
	}
	return node;
}

/*
================
figureWidth

The drawing width of a chart's container, in CSS pixels.
================
*/
function figureWidth( target ) {
	const node = byID( target ), style = getComputedStyle( node );
	// A figure on a hidden tab has no width; every figure spans the page
	// column, as the always-visible controls panel does.
	const width = node.clientWidth || byID( "controls" ).clientWidth;
	return width - parseFloat( style.paddingLeft ) - parseFloat( style.paddingRight );
}

/*
================
message
================
*/
function message( target, text ) {
	byID( target ).replaceChildren( element( "p", text, "economy-empty" ) );
}

/*
================
api
================
*/
function api( route, params ) {
	return requestJSON( `/api/economy/${route}?` + new URLSearchParams( params ) );
}

/*
================
selectTab
================
*/
function selectTab( tab ) {
	if ( !TABS.includes( tab ) ) tab = "overview";
	for ( const name of TABS ) {
		byID( "tab-" + name ).setAttribute( "aria-selected", String( name === tab ) );
		byID( "panel-" + name ).hidden = name !== tab;
	}
	if ( location.hash !== "#" + tab ) history.replaceState( null, "", "#" + tab );
}

/*
================
tile
================
*/
function tile( label, value, detail ) {
	const node = element( "div", "", "economy-tile" );
	node.append( element( "span", label, "economy-tile-label" ), element( "strong", value ) );
	if ( detail ) node.append( element( "small", detail ) );
	return node;
}

/*
================
drawOverview
================
*/
function drawOverview( grid, minted, burned, supply, distribution ) {
	const tiles = [];
	if ( minted && burned ) {
		const net = seriesTotal( minted ) - seriesTotal( burned );
		tiles.push(
			tile(
				"Net gold",
				(net > 0n ? "+" : "") + compactGold( net ),
				net > 0n ? "more minted than burned" : "more burned than minted"
			),
			tile( "Minted", compactGold( seriesTotal( minted ) ), "monsters, sales, quests, trade…" ),
			tile( "Burned", compactGold( seriesTotal( burned ) ), "shops, repairs, fees, tax…" )
		);
		const lines = [
			{ name: "Minted", tone: "mint", points: alignSeries( minted, grid, 0 ) },
			{ name: "Burned", tone: "burn", points: alignSeries( burned, grid, 0 ) }
		];
		byID( "chart-flow" ).innerHTML = lineChart( lines, {
			label: "Gold minted and burned per bucket",
			step: grid.step,
			width: figureWidth( "chart-flow" )
		} );
	} else message( "chart-flow", "Gold flows are unavailable from this server." );
	if ( supply ) {
		byID( "chart-supply" ).innerHTML = lineChart( [ {
			name: "Gold held",
			tone: "level",
			points: alignSeries( supply, grid, null )
		} ], {
			label: "Gold held by characters",
			step: grid.step,
			width: figureWidth( "chart-supply" ),
			connect: true,
			fit: true,
			empty: "No snapshot in this range yet."
		} );
	} else message( "chart-supply", "The money supply is unavailable from this server." );
	const gold = distribution?.gold;
	if ( gold ) {
		tiles.push(
			tile( "Gold held", compactGold( gold.total ), `${countText( gold.characters )} characters` ),
			tile(
				"Gini, accounts",
				Number.isFinite( gold.giniAccount14d ) ? gold.giniAccount14d.toFixed( 3 ) : "—",
				"accounts active in 14 days"
			),
			tile( "Top 1% hold", share( gold.top1Share ), `top 10%: ${share( gold.top10Share )}` )
		);
	}
	byID( "overview-tiles" ).replaceChildren( ...tiles );
}

/*
================
drawDistribution
================
*/
function drawDistribution( distribution, failure ) {
	if ( !distribution ) {
		byID( "snapshot-at" ).textContent = "No snapshot";
		message( "distribution", failure );
		message( "chart-plus", failure );
		message( "sox", failure );
		return;
	}
	const gold = distribution.gold ?? {};
	byID( "snapshot-at" ).textContent = "As of " + new Date( distribution.snapshotAt * 1000 ).toLocaleString();
	byID( "distribution" ).replaceChildren( table( [ "Measure", "Value" ], [
		[ "Gold held", groupDigits( gold.total ) ],
		[ "Characters", countText( gold.characters ) ],
		[ "Accounts active in 14 days", countText( gold.accounts14d ) ],
		[
			"Gini, accounts active in 14 days",
			Number.isFinite( gold.giniAccount14d ) ? gold.giniAccount14d.toFixed( 3 ) : "—"
		],
		[
			"Gini, all characters (alts and mules skew it)",
			Number.isFinite( gold.giniCharacter ) ? gold.giniCharacter.toFixed( 3 ) : "—"
		],
		[ "Top 1% share", share( gold.top1Share ) ],
		[ "Top 10% share", share( gold.top10Share ) ],
		[ "Median character", groupDigits( gold.p50 ) ],
		[ "90th percentile", groupDigits( gold.p90 ) ],
		[ "99th percentile", groupDigits( gold.p99 ) ],
		[ "Richest character", groupDigits( gold.max ) ]
	], [ 1 ] ) );
	byID( "chart-plus" ).innerHTML = barChart( plusBars( distribution.plus ), {
		label: "Equipment count by plus level",
		width: figureWidth( "chart-plus" ),
		empty: "No equipment counted yet."
	} );
	const sox = Array.isArray( distribution.sox ) ? distribution.sox : [];
	if ( sox.length === 0 ) message( "sox", "No seal items counted yet." );
	else {
		const soxTable = rows =>
			table(
				[ "Seal", "Star", "Moon", "Sun" ],
				rows.map( row => [ "Degree " + row.degree, ...[ row.star, row.moon, row.sun ].map( countText ) ] ),
				[ 1, 2, 3 ]
			);
		const parts = [ element( "h3", "Whole shard, warehouses included", "economy-subhead" ), soxTable( sox ) ];
		// The same count over accounts active in 14 days: what is in play now.
		if ( Array.isArray( distribution.soxAccounts14d ) ) {
			parts.push(
				element( "h3", "Accounts active in 14 days", "economy-subhead" ),
				soxTable( distribution.soxAccounts14d )
			);
		}
		byID( "sox" ).replaceChildren( ...parts );
	}
}

/*
================
drawFlows
================
*/
function drawFlows( flows, failure ) {
	if ( !flows ) {
		message( "flows", failure );
		return;
	}
	const rows = flowRows( meta, flows );
	if ( rows.length === 0 ) {
		message( "flows", "No ledger rows in this range yet." );
		return;
	}
	byID( "flows" ).replaceChildren( table(
		[ "Reason", "Kind", "Total", "Trend" ],
		rows.map( row => {
			const trend = element( "span", "", "economy-trend" );
			trend.innerHTML = sparkline( row.values.map( v => Number( v ) ) );
			const unit = row.unit && row.unit !== "gold" ? " " + row.unit : "";
			return [ reasonLabel( row.name ), row.kind, groupDigits( row.total ) + unit, trend ];
		} ),
		[ 2 ]
	) );
}

/*
================
drawRates

Kill and EXP rates per online hour, by UTC day and level band. Bands
under the contract's character floor are withheld server-side; the note
says how many, so a missing band is never mistaken for no play.
================
*/
function drawRates( rates, failure ) {
	if ( !rates ) {
		byID( "rates-note" ).textContent = "";
		message( "rates", failure );
		return;
	}
	const floor = Number.isFinite( rates.minCharacters ) ? rates.minCharacters : 3;
	const suppressed = Number.isFinite( rates.suppressed ) ? rates.suppressed : 0;
	byID( "rates-note" ).textContent = suppressed > 0 ?
		`Bands with fewer than ${floor} characters in a day are withheld: ${suppressed} withheld in this range.` :
		`Bands with fewer than ${floor} characters in a day are withheld; none were in this range.`;
	const rows = killStatRows( rates );
	if ( rows.length === 0 ) {
		message( "rates", "No counted kills in this range yet." );
		return;
	}
	byID( "rates" ).replaceChildren( table(
		[ "Day", "Band", "Characters", "Kills", "Kills / h", "EXP / h", "Skill EXP / h", "Online hours" ],
		rows.map( row => [
			row.dayText,
			row.label,
			countText( row.characters ),
			countText( row.kills ),
			rateText( row.killsPerHour ),
			rateText( row.expPerHour ),
			rateText( row.skillExpPerHour ),
			rateText( row.onlineHours )
		] ),
		[ 2, 3, 4, 5, 6, 7 ]
	) );
}

/*
================
censusCellText

One tier bucket: its items and holders, or "withheld" where the shard
held the bucket back under its floor.
================
*/
function censusCellText( cell ) {
	return cell ? `${countText( cell.items )} · ${countText( cell.holders )} holders` : "withheld";
}

/*
================
drawCensus

The newest census: when it was taken, the degree table, and where the
seals sit. Buckets under the shard's holder floor arrive withheld; the
note says how many, so a withheld tier is never mistaken for none.
================
*/
function drawCensus() {
	const note = byID( "census-note" ), target = byID( "census" ), places = byID( "census-locations" );
	if ( !censusReport ) {
		note.textContent = "";
		places.replaceChildren();
		message( "census", censusFailed || "No census has been run yet." );
		return;
	}
	const report = censusReport;
	const floor = Number.isFinite( report.minHolders ) ? report.minHolders : 3;
	const suppressed = Number.isFinite( report.suppressedCells ) ? report.suppressedCells : 0;
	const at = new Date( report.takenAt * 1000 ).toLocaleString();
	const took = Number.isFinite( report.elapsedMs ) ? ` in ${(report.elapsedMs / 1000).toFixed( 1 )}s` : "";
	note.textContent = (censusFailed ? censusFailed + " Showing the previous census. " : "") +
		`Counted ${at}${took}: ${countText( report.totalItems )} seal items in the shown buckets, held by ` +
		`${countText( report.charactersHolding )} characters and ${countText( report.accountsHolding )} accounts. ` +
		(suppressed > 0 ?
			`Buckets with fewer than ${floor} holders are withheld: ${suppressed} withheld.` :
			`Buckets with fewer than ${floor} holders are withheld; none were.`);
	const rows = censusRows( report );
	if ( rows.length === 0 ) {
		places.replaceChildren();
		message( "census", "The census found no seal items above the holder floor." );
		return;
	}
	target.replaceChildren( table(
		[ "Degree", "Star", "Moon", "Sun" ],
		rows.map( row => [
			"Degree " + row.degree,
			censusCellText( row.star ),
			censusCellText( row.moon ),
			censusCellText( row.sun )
		] )
	) );
	const locations = report.locations ?? {};
	places.replaceChildren(
		element( "p", "Where the seals sit", "economy-note" ),
		table(
			[ "Where", "Seal items" ],
			CENSUS_LOCATION_LABELS.map( ( [key, label] ) => [ label, countText( locations[key] ) ] ),
			[ 1 ]
		)
	);
}

/*
================
censusFailureText

The refusal said plainly; a 404 means the shard does not run the
census at all.
================
*/
function censusFailureText( error ) {
	if ( (error?.message ?? "").includes( "HTTP 404" ) ) {
		return "This shard does not run the seal census (SRO_SEAL_CENSUS is off).";
	}
	return error?.message ?? "Unavailable.";
}

/*
================
loadCensus

The shard's newest census, if it has one.
================
*/
async function loadCensus() {
	try {
		censusReport = await api( "sealcensus", { shard: byID( "shard" ).value } );
		censusFailed = "";
	} catch ( error ) {
		censusReport = null;
		censusFailed = censusFailureText( error );
	}
	drawCensus();
}

/*
================
runCensus

One census now. The shard copies its whole store first, so the button
stays disabled until the answer lands.
================
*/
async function runCensus() {
	const button = byID( "census-run" );
	button.disabled = true;
	byID( "census-note" ).textContent = "Counting… the shard copies its store; this can take a while.";
	try {
		censusReport = await postJSON(
			"/api/economy/sealcensus?" + new URLSearchParams( { shard: byID( "shard" ).value } ),
			{},
			{ signal: AbortSignal.timeout( 300000 ) }
		);
		censusFailed = "";
	} catch ( error ) {
		censusFailed = censusFailureText( error );
	}
	button.disabled = false;
	drawCensus();
}

/*
================
drawTrade

The trade route's shape arrives with its first GameWorld implementation;
until then the tab says what it will hold.
================
*/
function drawTrade( trade ) {
	message(
		"trade",
		trade ?
			"This GameWorld serves trade data; its view lands with the trade contract." :
			"Trade runs, stars, route profit, steals, the weekly pool and fortress tax appear here once this GameWorld serves them."
	);
}

/*
================
drawFlags
================
*/
function drawFlags( shard, result, failure ) {
	const count = byID( "flag-count" );
	if ( !result ) {
		count.hidden = true;
		message( "flags", failure );
		return;
	}
	const flags = Array.isArray( result.flags ) ? result.flags : [];
	count.hidden = flags.length === 0;
	count.textContent = String( flags.length );
	if ( flags.length === 0 ) {
		message( "flags", "No flags in this range." );
		return;
	}
	byID( "flags" ).replaceChildren( ...flags.map( flag => {
		const card = element( "article", "", "economy-flag" );
		const head = element( "div", "", "economy-flag-head" );
		head.append(
			element( "strong", flagLabel( flag.rule ) ),
			element( "time", new Date( flag.at * 1000 ).toLocaleString() )
		);
		const facts = document.createElement( "dl" );
		const fact = ( name, value ) => {
			if ( value === null || value === undefined || value === "" ) return;
			facts.append( element( "dt", name ), element( "dd", value ) );
		};
		fact( "Value", groupDigits( toBig( flag.value ) ) );
		fact( "Threshold", groupDigits( toBig( flag.threshold ) ) );
		fact( "Item", flag.ref );
		fact( "Division", flag.division );
		card.append( head, facts );
		if ( flag.character?.name ) {
			const link = element( "a", "Inspect " + flag.character.name + " in Player recovery", "button" );
			link.href = recoveryLink( shard, flag.character.name );
			card.append( link );
		}
		return card;
	} ) );
}

/*
================
failureText
================
*/
function failureText( reason ) {
	return reason?.message ?? "Unavailable.";
}

/*
================
paint

Draws the last loaded answers at the page's current width: after a load,
and again when the width changes (a phone turned, a window resized), so a
chart's text keeps its real size.
================
*/
function paint() {
	if ( !loaded ) return;
	const { shard, grid, minted, burned, supply, distribution, flows, flags, trade, rates } = loaded;
	const value = result => result.status === "fulfilled" ? result.value : null;
	// A malformed answer breaks only its own section.
	const draw = ( targets, paint ) => {
		try {
			paint();
		} catch ( error ) {
			for ( const target of targets ) message( target, "Could not draw this answer: " + error.message );
		}
	};
	draw(
		[ "chart-flow", "chart-supply" ],
		() => drawOverview( grid, value( minted ), value( burned ), value( supply ), value( distribution ) )
	);
	draw(
		[ "distribution", "chart-plus", "sox" ],
		() => drawDistribution( value( distribution ), failureText( distribution.reason ) )
	);
	draw( [ "flows" ], () => drawFlows( value( flows ), failureText( flows.reason ) ) );
	draw( [ "trade" ], () => drawTrade( value( trade ) ) );
	draw( [ "rates" ], () => drawRates( value( rates ), failureText( rates.reason ) ) );
	draw( [ "flags" ], () => drawFlags( shard, value( flags ), failureText( flags.reason ) ) );
}

/*
================
load
================
*/
async function load() {
	const current = ++generation;
	const shard = byID( "shard" ).value, division = byID( "division" ).value;
	const grid = rangeWindow( byID( "range" ).value, Date.now() / 1000 );
	const base = { shard, ...(division ? { division } : {}) };
	const span = { from: String( grid.from ), to: String( grid.to ) };
	const bucketed = { ...base, ...span, bucket: grid.bucket };
	status.textContent = "Loading…";
	const [minted, burned, supply, distribution, flows, flags, trade, rates] = await Promise.allSettled( [
		api( "series", { ...bucketed, metric: "gold_minted" } ),
		api( "series", { ...bucketed, metric: "gold_burned" } ),
		api( "series", { ...bucketed, metric: "gold_total" } ),
		api( "distribution", base ),
		api( "flows", bucketed ),
		api( "flags", { shard, ...span } ),
		api( "trade", { shard, ...span, bucket: grid.bucket } ),
		api( "killstats", { shard, ...span } )
	] );
	if ( current !== generation ) return;
	loaded = { shard, grid, minted, burned, supply, distribution, flows, flags, trade, rates };
	paintedWidth = byID( "controls" ).clientWidth;
	paint();
	const ledger = meta?.ledgerThrough ? new Date( meta.ledgerThrough * 1000 ).toLocaleString() : "unknown";
	status.textContent = minted.status === "rejected" ?
		"Gold flows: " + failureText( minted.reason ) :
		`Ledger complete through ${ledger}. Buckets are ${grid.bucket === "1h" ? "hours" : "days"} in UTC.`;
	// The census stands outside the ranged loads: newest report only.
	void loadCensus();
}

/*
================
loadMeta

The server's reasons and divisions; the division list is rebuilt per server.
================
*/
async function loadMeta() {
	const shard = byID( "shard" ).value;
	meta = null;
	try {
		meta = await api( "meta", { shard } );
	} catch ( error ) {
		status.textContent = error.message;
	}
	const select = byID( "division" ), kept = select.value;
	select.replaceChildren(
		new Option( "All divisions", "" ),
		...(Array.isArray( meta?.divisions ) ? meta.divisions : []).map( name =>
			new Option( String( name ), String( name ) )
		)
	);
	select.value = [ ...select.options ].some( option => option.value === kept ) ? kept : "";
}

/*
================
init
================
*/
async function init() {
	for ( const range of RANGES ) byID( "range" ).append( new Option( range.label, range.id ) );
	try {
		const saved = localStorage.getItem( RANGE_KEY );
		if ( RANGES.some( range => range.id === saved ) ) byID( "range" ).value = saved;
		else byID( "range" ).value = "7d";
	} catch {
		byID( "range" ).value = "7d";
	}
	selectTab( location.hash.slice( 1 ) );
	let config;
	try {
		config = await requestJSON( "/api/console" );
	} catch ( error ) {
		status.textContent = error.message;
		return;
	}
	byID( "shard" ).replaceChildren( ...config.shards.map( shard => new Option( shard.name, shard.id ) ) );
	if ( !config.economy ) {
		status.textContent = "Economy reads are not configured on this console (economyTokenFile).";
		return;
	}
	await loadMeta();
	await load();
}

for ( const name of TABS ) byID( "tab-" + name ).addEventListener( "click", () => selectTab( name ) );
window.addEventListener( "hashchange", () => selectTab( location.hash.slice( 1 ) ) );
window.addEventListener( "resize", () => {
	const width = byID( "controls" ).clientWidth;
	if ( Math.abs( width - paintedWidth ) < 24 ) return;
	paintedWidth = width;
	paint();
} );
byID( "controls" ).addEventListener( "submit", event => event.preventDefault() );
byID( "shard" ).addEventListener( "change", async () => {
	await loadMeta();
	await load();
} );
byID( "division" ).addEventListener( "change", () => void load() );
byID( "census-run" ).addEventListener( "click", () => void runCensus() );
byID( "range" ).addEventListener( "change", () => {
	try {
		localStorage.setItem( RANGE_KEY, byID( "range" ).value );
	} catch {}
	void load();
} );
void init();
