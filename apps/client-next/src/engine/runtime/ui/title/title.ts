/*
===========================================================================

title.ts - the title, login, server list and character dock screens

Projects the frontend snapshot into GPU quads, labels and interactive
controls, laid out from the native title resources (GDR_* nodes). It holds
only presentation state (fades, the server list slide); the session and the
credentials belong to ui.ts.

===========================================================================
*/
import { catalogMessage } from "@/engine/foundation/ui/catalog-message";
import type { AssetOwner } from "@/engine/contracts/assets";
import type { UiRect, UiControl, UiQuad } from "@/engine/contracts/ui";
import type { FrontendSnapshot } from "@/engine/contracts/frontend";
import type { ServerRecord, CharacterRecord } from "@/engine/contracts/session";
import { createTitleResources } from "./resources";
import { drawCreation } from "./creation";
import { titleGlyphs, titleTextBox, titleColoredText } from "@/engine/foundation/rendering/ui-glyphs";
import { fitUiGroup } from "@/engine/foundation/ui/layout";
import { buttonAccess, buttonTextColor } from "@/engine/foundation/ui/button-state";
import type { ButtonAccess } from "@/engine/foundation/ui/button-state";
import { dockSlot } from "@/engine/foundation/rendering/dock-slots";
import { screenPoint } from "@/engine/foundation/rendering/screen-point";
import type { TitleNode, TitleInteraction } from "./internal/title-contract";
/*
================
createTitleUi
================
*/
export function createTitleUi( assets: Pick<AssetOwner, "available" | "request" | "take" | "cancel">, base: string ) {
	const resources = createTitleResources( assets, base );
	let listAlpha = 0, lastTime: number | null = null;
	const nameAlpha = new Map<number, number>(), raceAlpha = [ 0, 0 ];
	return {
		message( value: string ) {
			return resources.data()?.text[value] ?? value;
		},
		catalog( key: string ) {
			return resources.data()?.text[key] ?? "";
		},
		render(
			state: FrontendSnapshot,
			w: number,
			h: number,
			account: string,
			password: string,
			servers: readonly ServerRecord[],
			selected: string,
			pending: boolean,
			serverList: boolean,
			input: TitleInteraction,
			roster: readonly CharacterRecord[] = [],
			selectedCharacter = ""
		) {
			resources.step();
			const data = resources.data(),
				raceTable = [ "create-arrival", "create", "create-return", "race-zoom", "loading-race" ].includes(
					state.phase
				),
				custom = [ "loading-create", "customize", "create-exit" ].includes( state.phase ),
				dock = [ "loading-dock", "dock-arrival", "dock", "departing", "title-exit", "title-logout" ].includes(
					state.phase
				),
				nodes = custom ?
					data?.creation[state.race ?? 0]?.controlsByName :
					dock || raceTable ?
					data?.dockNodes :
					data?.nodes;
			if ( !dock ) nameAlpha.clear();
			else {for ( const id of nameAlpha.keys() ) {
					if ( !roster.some( row => row.id === id ) ) nameAlpha.delete( id );
				}}
			const quads: UiQuad[] = [],
				controls: UiControl[] = [],
				paths: string[] = [],
				labels: { value: string; x: number; y: number; alpha: number; }[] = [];
			if ( data ) paths.push( data.font.image );
			if ( data && dock ) {
				for (
					const name of [
						"GDR_STA_WPOPUP",
						"GDR_STA_REMAINTIME",
						"GDR_BTN_DELETE",
						"GDR_BTN_RESTORE",
						"GDR_STA_TITLE",
						"GDR_STA_SCREENUP",
						"GDR_STA_SCREENDOWN",
						"GDR_STA_CHARINFO",
						"GDR_BTN_START",
						"GDR_BTN_BACK2",
						"GDR_BTN_CREATE",
						"GDR_BTN_BACK"
					]
				) {
					const path = nodes?.[name]?.ddj?.publicPath;
					if ( path ) {
						paths.push( path );
						if ( name.startsWith( "GDR_BTN_" ) ) {
							paths.push(
								path.replace( /\.png$/, "_focus.png" ),
								path.replace( /\.png$/, "_press.png" )
							);
						}
					}
				}
				for (
					const section of data.dockSections.filter( section =>
						[ "Info", "Warning", "Remain" ].includes( section.name )
					)
				) {
					for ( const node of section.nodes ) {
						if ( node.ddj ) {
							paths.push( node.ddj.publicPath );
							if ( node.name.startsWith( "GDR_BTN_" ) ) {
								paths.push(
									node.ddj.publicPath.replace( /\.png$/, "_focus.png" ),
									node.ddj.publicPath.replace( /\.png$/, "_press.png" )
								);
							}
						}
					}
				}
			}
			// Own the complete title texture set for the whole title lifetime, including
			// hidden panels and interaction states. Hover must never initiate residency.
			if ( data && !dock && !raceTable && !custom ) {
				paths.push( data.font.image );
				for ( const [name, n] of Object.entries( data.nodes ) ) {
					if ( !n.ddj ) continue;
					paths.push( n.ddj.publicPath );
					if ( name.startsWith( "GDR_BTN_" ) && name !== "GDR_BTN_THUMB" ) {
						paths.push(
							n.ddj.publicPath.replace( /\.png$/, "_focus.png" ),
							n.ddj.publicPath.replace( /\.png$/, "_press.png" )
						);
					}
				}
				paths.push(
					"/assets/images/Media_extracted/interface/outer/server_select.png",
					"/assets/images/Media_extracted/interface/outer/server_rollover.png"
				);
			}
			const scale = h / 1200,
				artScale = Math.min( scale, w / 800 ),
				top = Math.round( 172 * scale ),
				bottom = Math.round( 1030 * scale ),
				cy = top + Math.trunc( (bottom - top) / 2 ),
				cx = Math.trunc( w / 2 ),
				full: UiRect = [ 0, 0, w, h ];
			const playfield: UiRect = [ 8, top + 8, Math.max( 1, w - 16 ), Math.max( 1, bottom - top - 16 ) ];
			const elapsed = lastTime === null ? 0 : Math.max( 0, input.now - lastTime );
			lastTime = input.now;
			// 0x741BD0 loads float 0.5 at 0xBD5034 for both server/main chrome fades.
			listAlpha = Math.max( 0, Math.min( 1, listAlpha + (serverList ? 1 : -1) * elapsed / 500 ) );
			function fill( rect: UiRect, alpha = 1 ) {
				quads.push( { rect, color: [ 0, 0, 0, alpha ], uv: [ 0, 0, 1, 1 ], clip: full, texture: "" } );
			}
			function image( path: string, rect: UiRect, alpha = 1, uv: UiRect = [ 0, 0, 1, 1 ], clip: UiRect = full ) {
				paths.push( path );
				quads.push( { rect, color: [ 1, 1, 1, alpha ], uv, clip, texture: path } );
			}
			function picture( name: string, rect?: UiRect, alpha = 1 ) {
				const n = nodes?.[name];
				if ( n?.ddj ) {
					image(
						n.ddj.publicPath,
						rect ??
							[
								n.rect.x * artScale,
								n.rect.y * artScale,
								n.rect.width * artScale,
								n.rect.height * artScale
							],
						alpha
					);
				}
			}
			function text(
				value: string,
				rect: UiRect,
				alpha = 1,
				hAlign = 0,
				color: UiQuad["color"] = [ 1, 1, 1, 1 ],
				clip: UiRect = full,
				style?: TitleNode,
				overflow: "avoid-overlap" | "clip" = "avoid-overlap"
			) {
				if ( data ) {
					paths.push( data.font.image );
					quads.push(
						...titleGlyphs( data.font, value, rect, clip, [
							color[0],
							color[1],
							color[2],
							color[3] * alpha
						], { fontIndex: style?.fontIndex, hAlign, vAlign: style?.vAlign, overflow } )
					);
				}
			}
			function authoredText(
				n: TitleNode,
				value: string,
				rect: UiRect,
				alpha: number,
				access: ButtonAccess = "enabled"
			) {
				const c = n.fontColor,
					color = buttonTextColor(
						c ? [ c.r / 255, c.g / 255, c.b / 255, c.a / 255 ] : [ 1, 1, 1, 1 ],
						access
					);
				text( value, rect, alpha, n.hAlign ?? 0, color, full, n );
			}
			function local( name: string, parent: UiRect ): UiRect {
				const r = nodes![name]!.rect;
				return [ parent[0] + r.x, parent[1] + r.y, r.width, r.height ];
			}
			function button(
				name: string,
				id: string,
				rect: UiRect,
				alpha: number,
				interactive: boolean,
				inhibited = false,
				unavailable = false
			) {
				const n = nodes![name]!,
					path = n.ddj!.publicPath,
					access = buttonAccess( inhibited, unavailable ),
					disabled = access !== "enabled";
				const focused = input.hover === id || input.focus === id, pressed = input.pressed === id && !disabled;
				const focusPath = path.replace( /\.png$/, "_focus.png" ),
					pressPath = path.replace( /\.png$/, "_press.png" );
				paths.push( path, focusPath, pressPath );
				image( pressed ? pressPath : !disabled && focused ? focusPath : path, rect, alpha );
				const caption = n.text ? data!.text[n.text] ?? n.text : "";
				if ( caption ) {
					authoredText(
						n,
						caption,
						[ rect[0] + (pressed ? 1 : 0), rect[1] + (pressed ? 1 : 0), rect[2], rect[3] ],
						alpha,
						access
					);
				}
				if ( interactive ) {
					controls.push( {
						id,
						kind: "button",
						label: caption || (id === "native:server-prev" ?
							"Previous servers" :
							id === "native:server-next" ?
							"Next servers" :
							"Select server"),
						rect,
						disabled
					} );
				}
			}
			fill( [ 0, 0, w, top ] );
			fill( [ 0, bottom, w, h - bottom ] );
			// 0x727720 scales authored bars by width/1600 and height/1200. Splitting fixed-width
			// halves left uncovered black regions at widescreen aspect ratios.
			const up = nodes?.GDR_STA_SCREENUP?.ddj?.publicPath, down = nodes?.GDR_STA_SCREENDOWN?.ddj?.publicPath;
			if ( up ) image( up, [ 0, 0, w, top ] );
			if ( down ) image( down, [ 0, bottom, w, top ] );
			const phase = state.phase;
			if ( custom ) {
				for ( let i = 0; i < quads.length; i++ ) quads[i] = { ...quads[i]!, layer: "background" };
				const layout = data?.creation[state.race ?? 0];
				if ( layout && data && state.creation ) {
					const output = drawCreation(
						layout,
						data.font,
						data.text,
						state.creation,
						input,
						w,
						h,
						phase === "customize"
					);
					quads.push( ...output.quads );
					controls.push( ...output.controls );
					paths.push( ...output.paths );
				}
				if ( phase === "loading-create" ) fill( full );
				else if ( phase === "create-exit" ) fill( full, state.alpha );
				else if ( state.creation?.phase === "accepted" ) fill( full, state.creation.alpha );
				else if ( state.elapsed < .5 ) fill( full, 1 - state.alpha );
				return { quads, controls, paths, labels, ready: !!layout, error: resources.error() };
			}
			if ( raceTable && data ) {
				const amount = phase === "create-arrival" ?
					Math.min( 1, state.elapsed / 5 ) :
					phase === "create-return" ?
					Math.max( 0, 1 - state.elapsed / 5 ) :
					1;
				picture( "GDR_STA_TITLE", undefined, 1 - amount );
				picture( "GDR_STA_REGIONTITLE", undefined, amount );
				button(
					"GDR_BTN_CANCEL",
					"frontend:back",
					[ w - 105, bottom + 27, 92, 41 ],
					amount,
					phase === "create"
				);
				for ( const [index, name] of [ "EUROPE", "CHINA" ].entries() ) {
					const a = Math.max(
						0,
						Math.min( 1, raceAlpha[index]! + (state.hoveredRace === index ? 1 : -1) * elapsed * .003 )
					);
					raceAlpha[index] = a;
					const n = nodes!["GDR_STA_" + name]!,
						center = state.raceCenters?.[index],
						point = center && state.camera ? screenPoint( state.camera, center, w, h ) : null;
					if ( !point ) continue;
					const parent: UiRect = [
						Math.max( 5, Math.round( point[0] - n.rect.width / 2 ) ),
						Math.round( point[1] ),
						n.rect.width,
						n.rect.height
					];
					picture( n.name, parent, a );
					for (
						const child of data.dockSections.find( s =>
							s.name === (index === 0 ? "Europe" : "China")
						)?.nodes ?? []
					) {
						const r = child.rect,
							area: UiRect = [ parent[0] + r.x, parent[1] + r.y, r.width, r.height ],
							value = data.text[child.text ?? ""] ?? "",
							color = child.fontColor!;
						quads.push(
							...titleTextBox( data.font, value, area, area, [
								color.r / 255,
								color.g / 255,
								color.b / 255,
								a
							], child )
						);
					}
				}
				if ( phase === "loading-race" ) fill( full );
				if ( phase === "race-zoom" ) fill( full, Math.min( 1, state.elapsed ) );
				return { quads, controls, paths, labels, ready: true, error: resources.error() };
			}
			if ( dock && data ) {
				const dockChromeAlpha = phase === "dock-arrival" ? state.alpha : 1;
				picture( "GDR_STA_TITLE", undefined, dockChromeAlpha );
				const active = phase === "dock" && !state.dialog,
					chosen = roster.find( row => row.name === selectedCharacter ),
					by = bottom + 27;
				pending = pending || !!state.cameraMoving;
				const firstQuad = quads.length, firstControl = controls.length;
				if ( chosen ) {
					button(
						"GDR_BTN_START",
						"enter",
						[ w - 313, by, 92, 41 ],
						1,
						active,
						pending,
						chosen.deletePending
					);
					button(
						chosen.deletePending ? "GDR_BTN_RESTORE" : "GDR_BTN_DELETE",
						chosen.deletePending ? "dock:restore" : "dock:delete",
						[ w - 209, by, 92, 41 ],
						1,
						active,
						pending
					);
					button( "GDR_BTN_BACK2", "dock:back", [ w - 105, by, 92, 41 ], 1, active, pending );
				} else {
					button(
						"GDR_BTN_CREATE",
						"frontend:create",
						[ w - 209, by, 92, 41 ],
						dockChromeAlpha,
						active,
						pending
					);
					button(
						"GDR_BTN_BACK",
						"frontend:leave",
						[ w - 105, by, 92, 41 ],
						dockChromeAlpha,
						active,
						pending
					);
				}
				fitUiGroup(
					quads,
					controls,
					firstQuad,
					firstControl,
					[ 8, bottom + 8, w - 16, h - bottom - 16 ],
					full
				);
				if ( chosen ) {
					const q = quads.length, c = controls.length, parent: UiRect = [ w - 271, top + 41, 228, 140 ];
					picture( "GDR_STA_CHARINFO", parent );
					const values: Record<string, string> = {
						GDR_STA_NAME: chosen.name,
						GDR_STA_LEVEL: String( chosen.level ),
						GDR_STA_EXP: chosen.experiencePercent === undefined ?
							"—" :
							chosen.experiencePercent.toFixed( 2 ) + "%",
						GDR_STA_SP: String( chosen.skillPoints ?? 0 )
					};
					for ( const node of data.dockSections.find( section => section.name === "Info" )?.nodes ?? [] ) {
						const r = node.rect, area: UiRect = [ parent[0] + r.x, parent[1] + r.y, r.width, r.height ];
						if ( node.ddj ) {
							const amount = node.name === "GDR_GAU_HP" ?
								Math.max(
									0,
									Math.min( 1, (chosen.currentHp ?? chosen.maxHp) / Math.max( 1, chosen.maxHp ) )
								) :
								node.name === "GDR_GAU_MP" ?
								Math.max(
									0,
									Math.min( 1, (chosen.currentMp ?? chosen.maxMp) / Math.max( 1, chosen.maxMp ) )
								) :
								1;
							image( node.ddj.publicPath, [ area[0], area[1], area[2] * amount, area[3] ], 1, [
								0,
								0,
								amount,
								1
							] );
						}
						const value = values[node.name] ?? data.text[node.text ?? ""];
						if ( value ) authoredText( node, value, area, 1 );
					}
					fitUiGroup( quads, controls, q, c, playfield, full );
				}
				if ( state.camera ) {
					roster.slice( 0, 4 ).forEach( ( row, index ) => {
						const alpha = Math.max(
							0,
							Math.min(
								1,
								(nameAlpha.get( row.id ) ?? 0) +
									(state.hoveredCharacter === row.id ? 1 : -1) * elapsed / 500
							)
						);
						nameAlpha.set( row.id, alpha );
						if ( !alpha ) return;
						const slot = dockSlot( index, roster.length ),
							point = screenPoint(
								state.camera!,
								[
									slot.x,
									slot.y + (row.deletePending ? 9 : 20) * row.visualLoadout.heightScale,
									slot.z
								],
								w,
								h
							);
						if ( !point ) return;
						const font = data.font.fonts["0"]!,
							width = Array.from( row.name ).reduce(
								( sum, c ) =>
									sum + (font.glyphs[String( c.codePointAt( 0 ) )] ?? font.glyphs["63"])!.advanceX,
								0
							),
							r: UiRect = [ Math.round( point[0] - width / 2 ), Math.round( point[1] - 7.5 ), width, 15 ];
						fill( [ r[0] - 1, r[1] - 1, r[2] + 2, r[3] + 2 ], .251 );
						text( row.name, r, alpha, 0, [ 1, 1, 1, 1 ], full, {
							id: 0,
							name: "dock-name",
							rect: { x: 0, y: 0, width, height: 15 },
							fontIndex: 0,
							vAlign: 0
						} );
					} );
				}
				if ( chosen?.deletePending ) {
					const parent: UiRect = [ Math.trunc( (w - 328) / 2 ), bottom - 102, 328, 92 ];
					picture( "GDR_STA_REMAINTIME", parent );
					const total = 7 * 24 * 3600000,
						start = Date.parse( chosen.deleteReservedAt ?? "" ),
						remaining = Number.isFinite( start ) ?
							Math.max( 0, Math.min( total, start + total - Date.now() ) ) :
							null;
					for ( const n of data.dockSections.find( s => s.name === "Remain" )?.nodes ?? [] ) {
						const r = n.rect, area: UiRect = [ parent[0] + r.x, parent[1] + r.y, r.width, r.height ];
						if ( n.ddj ) {
							const amount = n.name === "GDR_REMAING" ?
								(remaining === null ? 0 : 1 - remaining / total) :
								1;
							image( n.ddj.publicPath, [ area[0], area[1], area[2] * amount, area[3] ], 1, [
								0,
								0,
								amount,
								1
							] );
						}
						if ( n.text ) authoredText( n, data.text[n.text] ?? "", area, 1 );
					}
					if ( remaining !== null ) {
						const minutes = Math.max( 1, Math.ceil( remaining / 60000 ) ),
							values = [ Math.floor( minutes / 1440 ), Math.floor( minutes / 60 ) % 24, minutes % 60 ],
							area: UiRect = [ parent[0] + 18, parent[1] + 36, 292, 14 ];
						let index = 0;
						quads.push(
							...titleColoredText(
								data.font,
								data.text.UIO_MSG_CHAR_DEL_TIME!.replace( /%d/g, () => String( values[index++] ) ),
								area,
								area,
								[ 1, 1, 1, 1 ]
							)
						);
					}
				}
				if ( state.status || chosen?.deletePending ) {
					const node = nodes!.GDR_TEXT_MESSAGE!,
						status = state.status ?? { key: "UIO_STT_CHAR_DEL_WAITING", suffix: "" },
						area: UiRect = [ 26, bottom + 29, w - 26, 100 ],
						color = node.fontColor!;
					quads.push(
						...titleTextBox( data.font, catalogMessage( status, data.text[status.key] ), area, area, [
							color.r / 255,
							color.g / 255,
							color.b / 255,
							1
						], { ...node, vAlign: 0 } )
					);
				}
				if ( state.dialog ) {
					const dialog = state.dialog,
						a = dialog.alpha,
						parent: UiRect = [ Math.trunc( (w - 344) / 2 ), Math.trunc( (h - 192) / 2 ), 344, 192 ];
					fill( full, a * 128 / 255 );
					picture( "GDR_STA_WPOPUP", parent, a );
					controls.length = 0;
					const q = quads.length;
					for ( const n of data.dockSections.find( s => s.name === "Warning" )?.nodes ?? [] ) {
						const r = n.rect, area: UiRect = [ parent[0] + r.x, parent[1] + r.y, r.width, r.height ];
						const restore = dialog.kind === "restore-character",
							key = n.name === "GDR_BTN_WACCEPT" ?
								(restore ? "UIO_STT_CHAR_RECOVERY" : "UIO_SELCHAR_CTL_DELETE") :
								n.name === "GDR_TB_INFO" ?
								(restore ? "UIO_STT_CHAR_RECOVERY_CONFIRM" : "UIO_STT_CHAR_DEL_CONFIRM") :
								n.text;
						const value = n.name === "GDR_STA_WNAME" ? dialog.character : data.text[key ?? ""] ?? "";
						if ( n.ddj ) {
							const id = n.name === "GDR_BTN_WACCEPT" ? "dock:warning-accept" : "dock:warning-cancel",
								path = n.ddj.publicPath;
							paths.push(
								path,
								path.replace( /\.png$/, "_focus.png" ),
								path.replace( /\.png$/, "_press.png" )
							);
							image(
								input.pressed === id ?
									path.replace( /\.png$/, "_press.png" ) :
									input.hover === id ?
									path.replace( /\.png$/, "_focus.png" ) :
									path,
								area,
								a
							);
							controls.push( {
								id,
								kind: "button",
								label: value,
								rect: area,
								disabled: dialog.phase !== "open"
							} );
						}
						if ( n.name === "GDR_TB_INFO" ) {
							const c = n.fontColor!;
							quads.push(
								...titleTextBox(
									data.font,
									value,
									area,
									area,
									[ c.r / 255, c.g / 255, c.b / 255, a ],
									n
								)
							);
						} else authoredText( n, value, area, a );
					}
					fitUiGroup( quads, controls, q, 0, [ 8, 8, w - 16, h - 16 ], full );
				}
				if ( phase === "loading-dock" ) fill( full );
				if ( phase === "dock-arrival" ) fill( full, (1 - state.alpha) * 128 / 255 );
				if ( phase === "departing" ) fill( full, state.alpha );
				if ( phase === "title-exit" || phase === "title-logout" ) fill( full, state.alpha );
				return { quads, controls, paths, labels, ready: true, error: resources.error() };
			}
			if ( phase === "intro" || phase === "login-reveal" ) {
				const alpha = phase === "intro" ? 1 : 1 - state.alpha;
				fill( [ 0, 0, w, top ], alpha );
				fill( [ 0, bottom, w, h - bottom ], alpha );
			}
			const logo = nodes?.GDR_STA_BIGLOGO?.rect;
			if ( logo && state.logoAlpha > 0 ) {
				const s = Math.min( scale, (w - 16) / logo.width );
				picture( "GDR_STA_BIGLOGO", [
					(logo.x + logo.width / 2) * w / 1600 - logo.width * s / 2,
					(logo.y + logo.height / 2) * scale - logo.height * s / 2,
					logo.width * s,
					logo.height * s
				], state.logoAlpha );
			}
			if ( phase === "loading-title" || phase === "failed" || !data ) {
				fill( full );
				labels.push( {
					value: phase === "failed" ? "Unable to finish loading" : "Loading Silkroad Online…",
					x: cx - 100,
					y: cy,
					alpha: 1
				} );
			} else {
				text( "Ver 1.150", [ 5, h - 25, 80, 20 ], 1, 1, [ 1, 128 / 255, 128 / 255, 1 ] );
				if ( phase === "intro" ) {
					controls.push( { id: "frontend:reveal", kind: "button", label: "Continue to login", rect: full } );
				} else {
					const alpha = phase === "login-reveal" ? state.alpha : 1,
						mainAlpha = alpha * (1 - listAlpha),
						interactive = !serverList && phase !== "login-accepted";
					picture( "GDR_STA_TITLE", undefined, alpha );
					if ( mainAlpha > 0 ) {
						const firstQuad = quads.length, firstControl = controls.length;
						// Preserve 0x743B80's anchor where it fits; tall windows need a panel gap.
						picture( "GDR_STA_LOGO", [
							cx - 254 * scale,
							Math.min( cy - 114, cy + 6 - 172 * scale - 8 ),
							508 * scale,
							172 * scale
						], mainAlpha );
						const login: UiRect = [ cx - 144, cy + 6, 288, 140 ];
						picture( "GDR_STA_LOGINWINDOW", login, mainAlpha );
						for ( const name of [ "GDR_STATIC1", "GDR_STATIC2", "GDR_STATIC3" ] ) {
							const n = nodes![name]!;
							authoredText( n, data.text[n.text!] ?? "", local( name, login ), mainAlpha );
						}
						for (
							const [name, id, value, caption, kind] of [ [
								"GDR_EDIT_ID",
								"account",
								account,
								"Account",
								"text"
							], [ "GDR_EDIT_PASS", "password", password, "Password", "password" ] ] as const
						) {
							const n = nodes![name]!,
								r = local( name, login ),
								c = n.clientRect,
								display = kind === "password" ? "*".repeat( Math.min( value.length, 24 ) ) : value;
							const area: UiRect = c ?
								[
									r[0] + c.x,
									r[1] + c.y,
									Math.max( 0, r[2] - c.x - c.width ),
									Math.max( 0, r[3] - c.y - c.height )
								] :
								r;
							if ( interactive ) {
								controls.push( {
									id,
									kind,
									value,
									label: caption,
									rect: r,
									disabled: input.credentialsLocked === true
								} );
							}
							if ( input.focus === id ) {
								const font = data.font.fonts[String( n.fontIndex ?? 0 )]!,
									width = ( s: string ) =>
										Array.from( s ).reduce(
											( sum, c ) =>
												sum +
												(font.glyphs[String( c.codePointAt( 0 ) )] ?? font.glyphs["63"])!
													.advanceX,
											0
										);
								const line = font.recordHeight + 5,
									start = Math.min( display.length, input.selection?.[0] ?? display.length ),
									end = Math.min( display.length, input.selection?.[1] ?? start );
								const x = area[0] +
										(n.hAlign === 1 ? Math.floor( (area[2] - width( display )) / 2 ) : 0) +
										width( display.slice( 0, start ) ),
									y = area[1] + Math.floor( (area[3] - line) / 2 );
								if ( end > start ) {
									quads.push( {
										rect: [ x, y, width( display.slice( start, end ) ), line ],
										uv: [ 0, 0, 1, 1 ],
										clip: area,
										texture: "",
										color: [ .2, .4, .8, .6 * mainAlpha ]
									} );
								}
								if ( Math.floor( input.now / 500 ) % 2 === 0 ) {
									quads.push( {
										rect: [ x, y, 1, line ],
										uv: [ 0, 0, 1, 1 ],
										clip: area,
										texture: "",
										color: [ 1, 1, 1, mainAlpha ]
									} );
								}
							}
							text( display, area, mainAlpha, n.hAlign ?? 0, [ 1, 1, 1, 1 ], area, n, "clip" );
						}
						// Only a login attempt locks the server choice. A list refresh
						// must not: the overlay drops clicks on a disabled control, and
						// the reveal starts a refresh exactly when players reach LIST.
						button(
							"GDR_BTN_SERVER",
							"native:servers",
							local( "GDR_BTN_SERVER", login ),
							mainAlpha,
							interactive,
							input.credentialsLocked === true
						);
						authoredText(
							nodes!.GDR_STA_SERVER!,
							servers.find( s => s.id === selected )?.name ?? "",
							local( "GDR_STA_SERVER", login ),
							mainAlpha
						);
						for (
							const [name, id, x] of [ [ "GDR_BTN_OK", "login", cx - 100 ], [
								"GDR_BTN_CANCEL",
								"logout",
								cx + 9
							] ] as const
						) {
							const n = nodes![name]!;
							button(
								name,
								id,
								[ x, cy + 156, n.rect.width, n.rect.height ],
								mainAlpha,
								interactive,
								pending
							);
						}
						fitUiGroup( quads, controls, firstQuad, firstControl, playfield, full );
					}
					if ( listAlpha > 0 ) {
						const box: UiRect = [ Math.max( 0, cx - 120 ), Math.max( 0, cy - 175 ), 240, 340 ],
							list = local( "GDR_LIST_SERVER", box ),
							a = alpha * listAlpha;
						const firstQuad = quads.length, firstControl = controls.length;
						picture( "GDR_STA_SERVERWINDOW", box, a );
						const offset = Math.min( Math.max( 0, servers.length - 13 ), input.offset );
						servers.slice( offset, offset + 13 ).forEach( ( server, i ) => {
							const rect: UiRect = [ list[0], list[1] + i * 20, 204, 20 ], id = "server:" + server.id;
							if ( input.draft === server.id || input.hover === id ) {
								image(
									"/assets/images/Media_extracted/interface/outer/server_" +
										(input.draft === server.id ? "select" : "rollover") + ".png",
									rect,
									a
								);
							}
							if ( serverList ) {
								controls.push( {
									id,
									label: server.name,
									kind: "button",
									rect,
									selected: input.draft === server.id,
									// Rows from the previous reply stay usable while a refresh
									// is in flight; the login request validates the server.
									disabled: !server.operating
								} );
							}
							text( server.name, [ rect[0] + 15, rect[1], 120, 20 ], a );
							const ratio = server.capacity > 0 ? server.onlinePlayers / server.capacity : 0;
							// 0x747E16 tests record +0x24 (operating), not the +0x28 name/test marker.
							const band = !server.operating ?
								[ "UIO_STT_SERVER_TEST", 0xc0bdff ] as const :
								ratio < Number( data.text.UIO_STT_SERVER_35 ) / 100 ?
								[ "UIO_STT_EXCELLENT", 0x6dffef ] as const :
								ratio < Number( data.text.UIO_STT_SERVER_75 ) / 100 ?
								[ "UIO_STT_CROWDEDNESS", 0xffda49 ] as const :
								ratio >= 1 ?
								[ "UIO_STT_SERVER_FULL", 0xffb541 ] as const :
								[ "UIO_STT_SERVER_MAX_FULL", 0xff5858 ] as const;
							text( data.text[band[0]] ?? "", [ rect[0] + 152, rect[1], 45, 20 ], a, 0, [
								(band[1] >> 16 & 255) / 255,
								(band[1] >> 8 & 255) / 255,
								(band[1] & 255) / 255,
								1
							] );
						} );
						if ( !servers.length ) {
							text( pending ? "Requesting server list…" : input.message || "No servers available.", [
								list[0] + 10,
								list[1],
								200,
								20
							], a );
						}
						const slider = local( "GDR_SLI_SERVER", box );
						button(
							"GDR_BTN_PREV",
							"native:server-prev",
							local( "GDR_BTN_PREV", slider ),
							a,
							serverList,
							false,
							offset === 0
						);
						button(
							"GDR_BTN_NEXT",
							"native:server-next",
							local( "GDR_BTN_NEXT", slider ),
							a,
							serverList,
							false,
							offset + 13 >= servers.length
						);
						picture( "GDR_BTN_THUMB", [
							slider[0],
							slider[1] + 20 + (servers.length > 13 ? 246 * offset / (servers.length - 13) : 0),
							20,
							20
						], a );
						button(
							"GDR_BTN_SACCEPT",
							"native:server-accept",
							[ box[0] + 21, box[1] + 350, 91, 40 ],
							a,
							serverList,
							false,
							!servers.some( s => s.id === input.draft && s.operating )
						);
						button(
							"GDR_BTN_SCANCEL",
							"native:server-cancel",
							[ box[0] + 130, box[1] + 350, 91, 40 ],
							a,
							serverList
						);
						fitUiGroup( quads, controls, firstQuad, firstControl, playfield, full );
					}
					if ( phase === "login-accepted" ) fill( full, state.alpha );
					if ( input.message ) {
						const node = nodes!.GDR_TEXT_MESSAGE!;
						authoredText( { ...node, vAlign: 0 }, data.text[input.message] ?? input.message, [
							9,
							bottom + 24,
							w,
							73
						], 1 );
					}
				}
			}
			return { quads, controls, paths, labels, ready: !!data, error: resources.error() };
		},
		dispose() {
			resources.dispose();
		}
	};
}
