/*
===========================================================================

economy-view.js - the Economy page's formatting, models and charts

Pure functions over the GameWorld economy contract v1 (server/economy.mjs),
so the page and its tests read the same rules. Gold arrives as decimal
strings (it overflows a JS number) and is summed and printed as BigInt;
only chart geometry converts to a number. Every chart is an SVG string on
one axis, with one hit column per bucket carrying its values as a native
tooltip, and every label is escaped.

===========================================================================
*/

// The windows the page offers: 1h buckets up to the contract's 30-day
// hourly retention (30 x 24 = 720, its bucket cap), daily beyond.
export const RANGES = Object.freeze( [
	{ id: "24h", label: "24 hours", seconds: 86400, bucket: "1h" },
	{ id: "7d", label: "7 days", seconds: 7 * 86400, bucket: "1h" },
	{ id: "30d", label: "30 days", seconds: 30 * 86400, bucket: "1h" },
	{ id: "90d", label: "90 days", seconds: 90 * 86400, bucket: "1d" },
	{ id: "365d", label: "1 year", seconds: 365 * 86400, bucket: "1d" }
] );

const KIND_ORDER = [ "mint", "burn", "transfer" ];

const FLAG_RULES = Object.freeze( {
	net_mint_p95: "Net gold minted above its 7-day p95",
	char_gold_spike: "Character gold spike",
	unit_drift: "Item balance drift (weak signal until item serials exist)"
} );

// Charts draw at the width they are shown (options.width, CSS pixels), so
// axis text stays its real size on a phone; 720 when none is given.
const DEFAULT_CHART_WIDTH = 720, MIN_CHART_WIDTH = 280;
const CHART_HEIGHT = 200, PAD_LEFT = 64, PAD_RIGHT = 12, PAD_TOP = 14, PAD_BOTTOM = 26;

/*
================
escapeHTML
================
*/
export function escapeHTML( value ) {
	return String( value ).replace( /[&<>"']/g, c => `&#${c.charCodeAt( 0 )};` );
}

/*
================
toBig

A contract amount (decimal string, possibly negative) as a BigInt; anything
malformed is zero rather than a thrown page.
================
*/
export function toBig( value ) {
	return typeof value === "string" && /^-?\d{1,40}$/.test( value ) ? BigInt( value ) : 0n;
}

/*
================
groupDigits
================
*/
export function groupDigits( value ) {
	const big = typeof value === "bigint" ? value : toBig( value );
	const sign = big < 0n ? "-" : "", digits = (big < 0n ? -big : big).toString();
	return sign + digits.replace( /\B(?=(\d{3})+(?!\d))/g, "," );
}

/*
================
countText

A count (characters, items) as grouped digits; a value that is not a
finite number reads as a dash rather than throwing.
================
*/
export function countText( value ) {
	const n = Number( value );
	return Number.isFinite( n ) ? groupDigits( BigInt( Math.round( n ) ) ) : "—";
}

/*
================
compactGold

Short form for tiles and axes: 1.24 B, 830 M, 12.5 K; digits more
significant digits where neighbouring values are close (a fitted axis).
================
*/
export function compactGold( value, digits = 3 ) {
	const big = typeof value === "bigint" ? value : toBig( value );
	const n = Number( big ), size = Math.abs( n );
	for ( const [limit, suffix] of [ [ 1e12, " T" ], [ 1e9, " B" ], [ 1e6, " M" ], [ 1e3, " K" ] ] ) {
		if ( size >= limit ) {
			const scaled = n / limit;
			return (Math.abs( scaled ) >= 10 ** (digits - 1) ? scaled.toFixed( 0 ) : scaled.toPrecision( digits )) +
				suffix;
		}
	}
	return String( n );
}

/*
================
rangeWindow

The from/to/bucket of a named range ending at now (unix seconds), with
from aligned down to its bucket so neighbouring loads share buckets.
================
*/
export function rangeWindow( id, now ) {
	const range = RANGES.find( row => row.id === id ) ?? RANGES[1];
	const step = range.bucket === "1h" ? 3600 : 86400;
	const to = Math.floor( now );
	const from = Math.floor( (to - range.seconds) / step ) * step + step;
	return { from, to, bucket: range.bucket, step };
}

/*
================
reasonLabel

A ledger reason by its name; names the console does not know yet are shown
as they are (the contract adds reasons without a version bump).
================
*/
export function reasonLabel( name ) {
	const text = String( name ).replaceAll( "_", " " );
	return text.charAt( 0 ).toUpperCase() + text.slice( 1 );
}

/*
================
flagLabel
================
*/
export function flagLabel( rule ) {
	return FLAG_RULES[rule] ?? reasonLabel( rule );
}

/*
================
flowRows

The flows table: one row per reason with its kind and unit from meta (an
unknown reason is kind "unknown"), its range total and its per-bucket
values, grouped mint / burn / transfer / other and, inside a group, by
the size of the total.
================
*/
export function flowRows( meta, flows ) {
	const known = new Map( (meta?.reasons ?? []).map( row => [ row.name, row ] ) );
	const rows = Object.entries( flows?.reasons ?? {} ).map( ( [name, values] ) => {
		const big = (Array.isArray( values ) ? values : []).map( toBig );
		return {
			name,
			kind: known.get( name )?.kind ?? "unknown",
			unit: known.get( name )?.unit ?? "",
			total: big.reduce( ( sum, v ) => sum + v, 0n ),
			values: big
		};
	} );
	const rank = kind => {
		const at = KIND_ORDER.indexOf( kind );
		return at < 0 ? KIND_ORDER.length : at;
	};
	return rows.sort( ( a, b ) => {
		if ( rank( a.kind ) !== rank( b.kind ) ) return rank( a.kind ) - rank( b.kind );
		const sizeA = a.total < 0n ? -a.total : a.total, sizeB = b.total < 0n ? -b.total : b.total;
		return sizeA === sizeB ? a.name.localeCompare( b.name ) : sizeA > sizeB ? -1 : 1;
	} );
}

/*
================
seriesTotal
================
*/
export function seriesTotal( series ) {
	return (series?.points ?? []).reduce( ( sum, point ) => sum + toBig( point.sum ), 0n );
}

/*
================
alignSeries

A series' points on the bucket grid from..to: a missing bucket is a gap
(null) for a level such as total gold, zero for a flow.
================
*/
export function alignSeries( series, window, fill ) {
	const at = new Map( (series?.points ?? []).map( point => [ point.t, point ] ) );
	const out = [];
	for ( let t = window.from; t <= window.to; t += window.step ) {
		const point = at.get( t );
		out.push( { t, value: point ? Number( toBig( point.sum ) ) : fill } );
	}
	return out;
}

/*
================
chartWidth
================
*/
function chartWidth( options ) {
	const width = Math.round( options.width ?? DEFAULT_CHART_WIDTH );
	return Number.isFinite( width ) ? Math.max( MIN_CHART_WIDTH, width ) : DEFAULT_CHART_WIDTH;
}

/*
================
niceMax

The axis top: the next 1, 2 or 5 step above the largest value.
================
*/
function niceMax( value ) {
	if ( value <= 0 ) return 1;
	const power = 10 ** Math.floor( Math.log10( value ) );
	for ( const step of [ 1, 2, 5, 10 ] ) {
		if ( value <= step * power ) return step * power;
	}
	return 10 * power;
}

/*
================
bucketTime
================
*/
export function bucketTime( t, step ) {
	const date = new Date( t * 1000 );
	return step >= 86400 ?
		date.toISOString().slice( 0, 10 ) :
		date.toISOString().slice( 0, 13 ).replace( "T", " " ) + ":00 UTC";
}

/*
================
lineChart

lines are { name, tone, points: [{ t, value }] } on the same bucket grid.
One y axis from zero (or the lowest value when it is negative); a 2px line
per series, a gap where a value is null (bridged with options.connect, for
a level sampled less often than its buckets; options.fit fits the axis to
the data instead of zero), and a hit column per bucket whose
tooltip lists every series. A legend names the series when there are two
or more; the chart title names a single one.
================
*/
export function lineChart( lines, options ) {
	const chartW = chartWidth( options );
	const grid = lines[0]?.points ?? [];
	if ( grid.length === 0 || lines.every( line => line.points.every( p => p.value === null ) ) ) {
		return `<p class="economy-empty">${escapeHTML( options.empty ?? "No data in this range yet." )}</p>`;
	}
	const values = lines.flatMap( line => line.points.map( p => p.value ).filter( v => v !== null ) );
	const low = Math.min( 0, ...values );
	let high = niceMax( Math.max( ...values ) );
	let bottom = low < 0 ? -niceMax( -low ) : 0;
	// A level (money held) is read for its change, so its axis fits the data
	// with a margin instead of starting at zero; flows keep the zero baseline.
	if ( options.fit ) {
		const least = Math.min( ...values ), most = Math.max( ...values );
		const margin = Math.max( (most - least) * .1, Math.abs( most ) * .001, 1 );
		bottom = least - margin;
		high = most + margin;
	}
	const plotW = chartW - PAD_LEFT - PAD_RIGHT, plotH = CHART_HEIGHT - PAD_TOP - PAD_BOTTOM;
	const x = i => PAD_LEFT + (grid.length === 1 ? plotW / 2 : i / (grid.length - 1) * plotW);
	const y = v => PAD_TOP + (high - v) / (high - bottom) * plotH;
	const ticks = [ bottom, bottom + (high - bottom) / 2, high ];
	// Enough digits that the three tick labels differ.
	let labelDigits = 3;
	while (
		labelDigits < 8 &&
		new Set( ticks.map( tick => compactGold( BigInt( Math.round( tick ) ), labelDigits ) ) ).size < ticks.length
	) labelDigits++;
	let svg = `<svg class="economy-chart" viewBox="0 0 ${chartW} ${CHART_HEIGHT}" role="img" aria-label="${
		escapeHTML( options.label )
	}">`;
	for ( const tick of ticks ) {
		svg += `<line class="grid" x1="${PAD_LEFT}" x2="${chartW - PAD_RIGHT}" y1="${y( tick )}" y2="${
			y( tick )
		}"/><text class="axis" x="${PAD_LEFT - 6}" y="${y( tick ) + 4}" text-anchor="end">${
			escapeHTML( compactGold( BigInt( Math.round( tick ) ), labelDigits ) )
		}</text>`;
	}
	if ( bottom < 0 ) {
		svg += `<line class="zero" x1="${PAD_LEFT}" x2="${chartW - PAD_RIGHT}" y1="${y( 0 )}" y2="${y( 0 )}"/>`;
	}
	svg += `<text class="axis" x="${PAD_LEFT}" y="${CHART_HEIGHT - 6}">${
		escapeHTML( bucketTime( grid[0].t, options.step ) )
	}</text><text class="axis" x="${chartW - PAD_RIGHT}" y="${CHART_HEIGHT - 6}" text-anchor="end">${
		escapeHTML( bucketTime( grid.at( -1 ).t, options.step ) )
	}</text>`;
	for ( const line of lines ) {
		let path = "", pen = false;
		line.points.forEach( ( p, i ) => {
			if ( p.value === null ) {
				pen = pen && options.connect === true;
				return;
			}
			path += `${pen ? "L" : "M"}${x( i ).toFixed( 1 )},${y( p.value ).toFixed( 1 )}`;
			pen = true;
		} );
		svg += `<path class="line tone-${line.tone}" d="${path}"/>`;
		if ( grid.length === 1 ) {
			svg += `<circle class="dot tone-${line.tone}" cx="${x( 0 )}" cy="${
				y( line.points[0].value ?? 0 )
			}" r="4"/>`;
		}
	}
	const column = grid.length === 1 ? plotW : plotW / (grid.length - 1);
	grid.forEach( ( p, i ) => {
		const tip = [ bucketTime( p.t, options.step ) ].concat(
			lines.map( line => {
				const value = line.points[i].value;
				return `${line.name}: ${value === null ? "no data" : groupDigits( BigInt( Math.round( value ) ) )}`;
			} )
		).join( "\n" );
		svg += `<rect class="hit" x="${(x( i ) - column / 2).toFixed( 1 )}" y="${PAD_TOP}" width="${
			column.toFixed( 2 )
		}" height="${plotH}"><title>${escapeHTML( tip )}</title></rect>`;
	} );
	svg += "</svg>";
	const legend = lines.length < 2 ?
		"" :
		`<ul class="economy-legend">${
			lines.map( line => `<li><span class="swatch tone-${line.tone}"></span>${escapeHTML( line.name )}</li>` )
				.join( "" )
		}</ul>`;
	return legend + svg;
}

/*
================
barChart

bars are { label, value } (counts); one tone, value labels above the
bars, a tooltip per bar.
================
*/
export function barChart( bars, options ) {
	const chartW = chartWidth( options );
	if ( bars.length === 0 || bars.every( bar => !bar.value ) ) {
		return `<p class="economy-empty">${escapeHTML( options.empty ?? "No data yet." )}</p>`;
	}
	const high = niceMax( Math.max( ...bars.map( bar => bar.value ) ) );
	const plotW = chartW - PAD_LEFT - PAD_RIGHT, plotH = CHART_HEIGHT - PAD_TOP - PAD_BOTTOM;
	const slot = plotW / bars.length, width = Math.max( 2, slot - 2 );
	let svg = `<svg class="economy-chart" viewBox="0 0 ${chartW} ${CHART_HEIGHT}" role="img" aria-label="${
		escapeHTML( options.label )
	}"><line class="grid" x1="${PAD_LEFT}" x2="${chartW - PAD_RIGHT}" y1="${PAD_TOP + plotH}" y2="${
		PAD_TOP + plotH
	}"/>`;
	bars.forEach( ( bar, i ) => {
		const h = bar.value / high * plotH, left = PAD_LEFT + i * slot + 1, top = PAD_TOP + plotH - h;
		svg += `<rect class="bar" x="${left.toFixed( 1 )}" y="${top.toFixed( 1 )}" width="${
			width.toFixed( 1 )
		}" height="${h.toFixed( 1 )}" rx="2"><title>${
			escapeHTML( `${bar.label}: ${countText( bar.value )}` )
		}</title></rect>`;
		svg += `<text class="axis" x="${(left + width / 2).toFixed( 1 )}" y="${
			CHART_HEIGHT - 8
		}" text-anchor="middle">${escapeHTML( bar.label )}</text>`;
	} );
	return svg + "</svg>";
}

/*
================
sparkline

A flow's per-bucket values as a small single-tone line, for table rows.
================
*/
export function sparkline( values ) {
	if ( values.length < 2 ) return "";
	const numbers = values.map( v => Number( v ) ), low = Math.min( 0, ...numbers ), high = Math.max( 1, ...numbers );
	const path = numbers.map( ( v, i ) =>
		`${i ? "L" : "M"}${(i / (numbers.length - 1) * 118 + 1).toFixed( 1 )},${
			(23 - (v - low) / (high - low) * 22).toFixed( 1 )
		}`
	).join( "" );
	return `<svg class="economy-spark" viewBox="0 0 120 24" aria-hidden="true"><path d="${path}"/></svg>`;
}

/*
================
plusBars

The +N equipment distribution as bars, +0 first.
================
*/
export function plusBars( plus ) {
	return Object.entries( plus ?? {} ).map( ( [level, count] ) => ({
		level: Number( level ),
		count: Number( count )
	}) )
		.filter( row => Number.isInteger( row.level ) && Number.isFinite( row.count ) )
		.sort( ( a, b ) => a.level - b.level )
		.map( row => ({ label: "+" + row.level, value: row.count }) );
}

/*
================
recoveryLink

The Player recovery view, prefilled with a flagged character.
================
*/
export function recoveryLink( shard, name ) {
	return "/?" + new URLSearchParams( { shard, character: name } ) + "#recovery";
}

/*
================
share
================
*/
export function share( value ) {
	return Number.isFinite( value ) ? (value * 100).toFixed( 1 ) + "%" : "—";
}

/*
================
dayLabel

A whole UTC day (unix seconds at its opening) as its ISO date.
================
*/
export function dayLabel( daySeconds ) {
	return new Date( daySeconds * 1000 ).toISOString().slice( 0, 10 );
}

/*
================
rateText

A per-hour rate with its digits grouped and at most two decimals;
a value that is not a finite number reads as a dash.
================
*/
export function rateText( value ) {
	const n = Number( value );
	if ( !Number.isFinite( n ) ) return "—";
	const rounded = Math.round( n * 100 ) / 100;
	const [whole, fraction] = Math.abs( rounded ).toFixed( 2 ).split( "." );
	const tail = fraction.replace( /0+$/, "" );
	return (rounded < 0 ? "-" : "") + groupDigits( BigInt( whole ) ) + (tail ? "." + tail : "");
}

/*
================
killStatRows

The killstats answer flattened for the Rates table: one row per
(day, band), newest day first, bands low to high inside a day. A
malformed answer yields no rows rather than a thrown page.
================
*/
export function killStatRows( body ) {
	const days = Array.isArray( body?.days ) ? body.days : [];
	const rows = [];
	for ( const day of days ) {
		const bands = Array.isArray( day?.bands ) ? day.bands : [];
		for ( const band of bands ) {
			rows.push( {
				day: Number( day.day ),
				dayText: dayLabel( Number( day.day ) ),
				band: Number( band.band ),
				label: String( band.label ?? "" ),
				characters: band.characters,
				kills: band.kills,
				killsPerHour: band.killsPerOnlineHour,
				expPerHour: band.expPerOnlineHour,
				skillExpPerHour: band.skillExpPerOnlineHour,
				onlineHours: band.onlineHours
			} );
		}
	}
	return rows.sort( ( a, b ) => b.day - a.day || a.band - b.band );
}
