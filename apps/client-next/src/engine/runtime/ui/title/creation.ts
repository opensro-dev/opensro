/*
===========================================================================

creation.ts - the character creation window's native layout and controls

Builds the CPSCharacterCreate quads and semantic controls (race, figure,
scale, weapon and protector choices, rotation and explanation panels) from
the authored layout. It owns presentation only; the creation selection and
its starter items live in foundation/ui/character-create.ts.

===========================================================================
*/
import { catalogMessage } from "@/engine/foundation/ui/catalog-message";
import type { TitleLayout, TitleNode, TitleInteraction } from "./internal/title-contract";

import type { CreationSnapshot } from "@/engine/contracts/frontend";
import type { UiQuad, UiRect, UiControl } from "@/engine/contracts/ui";
import { titleGlyphs, titleTextBox, type FontAtlas } from "@/engine/foundation/rendering/ui-glyphs";
import { creationRange, creationModelCodename, creationProtectors } from "@/engine/foundation/ui/character-create";
import { fitUiGroup } from "@/engine/foundation/ui/layout";
import { buttonAccess, buttonTextColor } from "@/engine/foundation/ui/button-state";
import type { ButtonAccess } from "@/engine/foundation/ui/button-state";
/*
================
drawCreation
================
*/
export function drawCreation(
	layout: TitleLayout,
	font: FontAtlas,
	catalog: Record<string, string>,
	state: CreationSnapshot,
	input: TitleInteraction,
	w: number,
	h: number,
	interactive: boolean
) {
	const quads: UiQuad[] = [],
		controls: UiControl[] = [],
		paths: string[] = [ font.image ],
		full: UiRect = [ 0, 0, w, h ],
		top = Math.round( 172 * h / 1200 ),
		bottom = Math.round( 1030 * h / 1200 );
	const nodes = layout.controlsByName,
		selection = state.selection,
		enabled = interactive && state.phase === "editing" && state.ready;
	function image( path: string, rect: UiRect, alpha = 1 ) {
		paths.push( path );
		quads.push( { rect, texture: path, color: [ 1, 1, 1, alpha ], clip: full, uv: [ 0, 0, 1, 1 ] } );
	}
	function text(
		node: TitleNode,
		value: string,
		rect: UiRect,
		alpha = 1,
		wrap = false,
		access: ButtonAccess = "enabled"
	) {
		const c = node.fontColor ?? { r: 255, g: 255, b: 255, a: 255 },
			tint = buttonTextColor( [ c.r / 255, c.g / 255, c.b / 255, c.a / 255 ], access ),
			color: UiQuad["color"] = [ tint[0], tint[1], tint[2], tint[3] * alpha ];
		quads.push(
			...(wrap ? titleTextBox : titleGlyphs)(
				font,
				value,
				rect,
				wrap || node.name === "GDR_EDIT_NAME" ? rect : full,
				color,
				{ ...node, overflow: node.name === "GDR_EDIT_NAME" ? "clip" : "avoid-overlap" }
			)
		);
	}
	function rect( node: TitleNode, parent: UiRect ): UiRect {
		return [ parent[0] + node.rect.x, parent[1] + node.rect.y, node.rect.width, node.rect.height ];
	}
	function panel( name: string, x: number, y: number ): UiRect {
		const n = nodes[name]!;
		const r: UiRect = [ x, y, n.rect.width, n.rect.height ];
		if ( n.ddj ) image( n.ddj.publicPath, r );
		return r;
	}
	function button( n: TitleNode, id: string, r: UiRect, disabled = !enabled, alpha = 1, path = n.ddj!.publicPath ) {
		const focus = path.replace( /\.png$/, "_focus.png" ), press = path.replace( /\.png$/, "_press.png" );
		paths.push( path, focus, press );
		image( !disabled && input.pressed === id ? press : !disabled && input.hover === id ? focus : path, r, alpha );
		const inset = n.clientRect ?? { x: 0, y: 0, width: 0, height: 0 },
			pressed = !disabled && input.pressed === id ? 1 : 0;
		const caption = catalog[n.text ?? ""] ?? "";
		if ( caption ) {
			text(
				n,
				caption,
				[
					r[0] + inset.x + pressed,
					r[1] + inset.y + pressed,
					r[2] - inset.x - inset.width,
					r[3] - inset.y - inset.height
				],
				alpha,
				false,
				buttonAccess( disabled )
			);
		}
		controls.push( { id, kind: "button", label: caption || id.replace( "create:", "" ), rect: r, disabled } );
	}
	// Hidden interaction textures belong to this screen, before the first hover.
	for ( const section of layout.sections ) {
		for ( const n of section.nodes ) {
			if ( n.ddj ) {
				paths.push( n.ddj.publicPath );
				if ( n.name.startsWith( "GDR_BTN_" ) && n.name !== "GDR_BTN_THUMB" ) {
					paths.push(
						n.ddj.publicPath.replace( /\.png$/, "_focus.png" ),
						n.ddj.publicPath.replace( /\.png$/, "_press.png" )
					);
				}
			}
		}
	}
	for ( const n of layout.sections.flatMap( section => section.nodes ) ) {
		const path = n.ddj?.publicPath;
		if ( path && /(man|woman)_(on|off)|zoomin/.test( path ) ) {
			for (
				const variant of [
					path.replace( /_(on|off)/, "_on" ),
					path.replace( /_(on|off)/, "_off" ),
					path.replace( "zoomin", "zoomout" )
				]
			) {
				paths.push(
					variant,
					variant.replace( /\.png$/, "_focus.png" ),
					variant.replace( /\.png$/, "_press.png" )
				);
			}
		}
	}
	const title = nodes.GDR_STA_TITLE!;
	if ( title.ddj ) {
		image( title.ddj.publicPath, [ title.rect.x, title.rect.y, title.rect.width, title.rect.height ] );
		quads[quads.length - 1] = { ...quads.at( -1 )!, layer: "background" };
	}
	let q = quads.length, c = controls.length;
	const custom = panel( "GDR_STA_CUSTOM", 75, Math.round( (bottom - top) / 2 + top - 155 ) );
	for ( const n of layout.sections.find( s => s.name === "Custom" )?.nodes ?? [] ) {
		const r = rect( n, custom );
		if ( n.name.startsWith( "GDR_STATIC" ) ) {
			text( n, catalog[n.text ?? ""] ?? "", r );
			continue;
		}
		if ( n.name === "GDR_EDIT_NAME" ) {
			const inset = n.clientRect ?? { x: 0, y: 0, width: 0, height: 0 },
				area: UiRect = [
					r[0] + inset.x,
					r[1] + inset.y,
					r[2] - inset.x - inset.width,
					r[3] - inset.y - inset.height
				];
			controls.push( {
				id: "create:name",
				kind: "text",
				label: catalog.UIO_NEWCHAR_STT_NAME ?? "Name",
				rect: r,
				value: selection.name,
				maxLength: 12,
				disabled: !enabled
			} );
			text( n, selection.name, area );
			if ( input.focus === "create:name" && Math.floor( input.now / 500 ) % 2 === 0 ) {
				const f = font.fonts[String( n.fontIndex ?? 0 )]!,
					advance = Array.from( selection.name.slice( 0, input.selection?.[0] ?? selection.name.length ) )
						.reduce(
							( sum, c ) => sum + (f.glyphs[String( c.codePointAt( 0 ) )] ?? f.glyphs["63"])!.advanceX,
							0
						);
				quads.push( {
					rect: [ area[0] + advance, area[1] + 2, 1, 14 ],
					color: [ 1, 1, 1, 1 ],
					texture: "",
					clip: area,
					uv: [ 0, 0, 1, 1 ]
				} );
			}
			continue;
		}
		if ( n.name === "GDR_BTN_CHECK" ) {
			button( n, "create:check", r );
			continue;
		}
		if ( n.name === "GDR_BTN_MALE" || n.name === "GDR_BTN_FEMALE" ) {
			const male = n.name === "GDR_BTN_MALE",
				on = selection.gender === (male ? 0 : 1),
				path = n.ddj!.publicPath.replace(
					/(man|woman)_(on|off)/,
					(male ? "man" : "woman") + "_" + (on ? "on" : "off")
				);
			button( n, male ? "create:male" : "create:female", r, !enabled, 1, path );
			continue;
		}
		if ( n.name.startsWith( "GDR_SLI_" ) ) {
			const key = n.name.slice( 8 ).toLowerCase() as "figure" | "height" | "volume" | "weapon" | "protector",
				[min, max] = creationRange( selection, key ),
				slider = Object.fromEntries(
					layout.sections.find( s => s.name === "Slider" )!.nodes.map( n => [ n.name, n ] )
				);
			button(
				slider.GDR_BTN_PREV!,
				"create:" + key + ":prev",
				[ r[0], r[1] + 2, 20, 20 ],
				!enabled || selection[key] <= min
			);
			button(
				slider.GDR_BTN_NEXT!,
				"create:" + key + ":next",
				[ r[0] + 120, r[1] + 2, 20, 20 ],
				!enabled || selection[key] >= max
			);
			const thumb = slider.GDR_BTN_THUMB!, offset = max > min ? (selection[key] - min) / (max - min) * 84 : 0;
			if ( thumb.ddj ) image( thumb.ddj.publicPath, [ r[0] + 20 + offset, r[1], 16, 24 ] );
			controls.push( {
				id: "create:" + key,
				kind: "range",
				label: catalog["UIO_NEWCHAR_STT_" + key.toUpperCase()] ?? key,
				rect: [ r[0] + 20, r[1], 100, 24 ],
				min,
				max,
				value: String( selection[key] ),
				disabled: !enabled || min === max
			} );
		}
	}
	fitUiGroup( quads, controls, q, c, [ 8, top + 8, w - 16, bottom - top - 16 ], full );
	q = quads.length;
	c = controls.length;
	const rotate = panel( "GDR_STA_ROTATE", w - 165, bottom - 69 );
	for ( const n of layout.sections.find( s => s.name === "Rotate" )?.nodes ?? [] ) {
		const id = n.name === "GDR_BTN_LROTATE" ?
			"create:left" :
			n.name === "GDR_BTN_RROTATE" ?
			"create:right" :
			"create:zoom";
		button(
			n,
			id,
			rect( n, rotate ),
			!enabled,
			1,
			id === "create:zoom" ?
				n.ddj!.publicPath.replace( "zoomin", state.zoom ? "zoomout" : "zoomin" ) :
				n.ddj!.publicPath
		);
	}
	fitUiGroup( quads, controls, q, c, [ 8, top + 8, w - 16, bottom - top - 16 ], full );
	const category = state.explain, eu = selection.race === 0;
	let titleKey = "", bodyKey = "";
	if ( category === "height" || category === "volume" ) {
		titleKey = "UIO_NEWCHAR_STT_" + category.toUpperCase();
		bodyKey = "UIO_NEWCHAR_EXPLANATION_" + category.toUpperCase();
	} else if ( category === "figure" ) {
		const model = creationModelCodename( selection );
		titleKey = model.replace( "CHAR_", "UIO_NEWCHAR_" ).replace( "CH_", "" ).replace( "WOMAN_", "FEMALE_" ).replace(
			"NECROMENCER",
			"NECROMANCER"
		);
		bodyKey = titleKey + "_EXPLANATION";
	} else if ( category === "weapon" ) {
		const kind = (eu ?
			" DAGGER ONEHANDSWORD TWOHANDSWORD DUELAXE CROSSBOW DARKSTAFF TWOHANDSTAFF HARP ONEHANDSTAFF" :
			" SWORD BLADE SPEAR TBLADE BOW").split( " " )[selection.weapon];
		titleKey = kind ? "UIO_NEWCHAR_STT_" + (eu ? "EU_" : "") + kind : "UIO_NEWCHAR_STT_WEAPON";
		bodyKey = "UIO_NEWCHAR_EXPLANATION_" + (kind ?? "ARMY_SELECT");
		if ( !kind ) {
			bodyKey = "UIO_NEWCHAR_EXPLANATION_ARMY_SELECT";
		}
	} else {
		const armor = creationProtectors( selection )[selection.protector - 1],
			kind = armor === "HEAVY" ?
				"HEAVY_ARMOR" :
				armor === "LIGHT" ?
				"LIGHT_ARMOR" :
				armor ?
				(eu ? "ROBE" : "CLOTHES") :
				null;
		titleKey = kind ? "UIO_NEWCHAR_STT_" + (eu ? "EU_" : "") + kind : "UIO_NEWCHAR_STT_PROTECTOR";
		bodyKey = kind ? "UIO_NEWCHAR_EXPLANATION_" + (eu ? "EU_" : "") + kind : "UIO_NEWCHAR_EXPLANATION_ARMOR_SELECT";
	}
	q = quads.length;
	c = controls.length;
	const explain = panel( "GDR_STA_EXPLAIN", rotate[0] - (eu ? 69 : 59), rotate[1] - (eu ? 270 : 219) );
	for ( const n of layout.sections.find( s => s.name === "Explain" )?.nodes ?? [] ) {
		text(
			n,
			catalog[n.name === "GDR_STA_EXPLAINNAME" ? titleKey : bodyKey] ?? "",
			rect( n, explain ),
			1,
			n.name === "GDR_TEXT_EXPLAIN"
		);
	}
	fitUiGroup( quads, controls, q, c, [ 8, top + 8, w - 16, bottom - top - 16 ], full );
	q = quads.length;
	c = controls.length;
	button( nodes.GDR_BTN_OK!, "create:ok", [ w - 209, bottom + 27, 92, 41 ] );
	button(
		nodes.GDR_BTN_BACK!,
		"frontend:back",
		[ w - 105, bottom + 27, 92, 41 ],
		!interactive || ![ "editing", "checking" ].includes( state.phase )
	);
	fitUiGroup( quads, controls, q, c, [ 8, bottom + 8, w - 16, h - bottom - 16 ], full );
	if ( state.status ) {
		text(
			{ ...nodes.GDR_TEXT_MESSAGE!, vAlign: 0 },
			catalogMessage( state.status, catalog[state.status.key] ),
			[ 26, bottom + 29, w - 26, 100 ],
			1,
			true
		);
	}
	if ( state.phase === "confirming" || state.phase === "dismissing" || state.phase === "submitting" ) {
		const a = state.alpha;
		quads.push( { rect: full, color: [ 0, 0, 0, a * 128 / 255 ], texture: "", clip: full, uv: [ 0, 0, 1, 1 ] } );
		controls.length = 0;
		const n = nodes.GDR_STA_WCREATE!,
			parent: UiRect = [
				Math.trunc( (w - n.rect.width) / 2 ),
				Math.trunc( (h - n.rect.height) / 2 ),
				n.rect.width,
				n.rect.height
			];
		if ( n.ddj ) image( n.ddj.publicPath, parent, a );
		for ( const n of layout.sections.find( s => s.name === "WCreate" )?.nodes ?? [] ) {
			const r = rect( n, parent );
			if ( n.ddj ) {
				button(
					n,
					n.name === "GDR_BTN_WCREATE" ? "create:confirm" : "create:confirm-cancel",
					r,
					state.phase !== "confirming" || a < 1,
					a
				);
			} else text( n, n.name === "GDR_STA_WNAME" ? selection.name : catalog[n.text ?? ""] ?? "", r, a );
		}
	}
	return { quads, controls, paths };
}
