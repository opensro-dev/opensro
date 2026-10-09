/*
===========================================================================

session.ts - the title and character-select session owner

Owns sign-in, session restoration, the server list, the character roster and
character operations, and hands a chosen character to the world session.
Every request goes through http.ts; publish() is the only state writer, and
request generations keep a replaced request from publishing. Once a server
answers that it speaks another release protocol, every published state
carries releaseOutdated so the page offers the newer release.

===========================================================================
*/
import { createWorldSession } from "./world/world";
import { createSessionDecoder } from "./decode/decode";
import { createSessionHttp } from "./http/http";
import type { SessionOwner, SessionState } from "@/engine/contracts/session";
import type { ClientIncident } from "@/engine/contracts/network";
import { MAX_STARTING_WAIT_MS, STARTING_CODE, serverStarting } from "@/engine/foundation/session/server-starting";

// How long a failure report may take; it never holds up the session ending.
const INCIDENT_TIMEOUT_MS = 5000;
const INCIDENT_QUEUE_LIMIT = 8;
const INCIDENT_MAX_ATTEMPTS = 4;
const INCIDENT_RETRY_MS = 6000;
const RELEASE_OUTDATED_MESSAGE = "A newer version of the game is available. Refresh the page to continue.";
/*
================
createSession
================
*/
export function createSession(): SessionOwner {
	const decoder = createSessionDecoder();
	const http = createSessionHttp();
	let crestPrefix: number | undefined;
	let restoreAttempted = false, browserBase: string | undefined, logoutPending = false;
	let resumeCharacter: string | undefined, restoringWorld = false;
	// A title request answered PROCESS_STARTING is repeated (server-starting.ts):
	// the command to repeat, when, and since when the server has been starting.
	let lastTitleCommand: Parameters<SessionOwner["command"]>[0] | undefined;
	let startingRetry: { dueMs: number; command: Parameters<SessionOwner["command"]>[0]; } | null = null;
	let startingSince: number | undefined, repeatingStart = false;
	let generation = 0, disposed = false, controller: AbortController | null = null;
	let state: SessionState = Object.freeze( { phase: "signed-out", revision: 0 } ), dirty = false;
	let identity: {
		apiBase: string;
		token: string;
		divisionId: string;
		transportUrl: string;
		nativeServerName?: string;
	} | null = null;
	let completion: {
		generation: number;
		value?: {
			httpOk: boolean;
			body: unknown;
		};
		error?: string;
		expectedShard: string;
		kind: "login" | "restore" | "logout" | "return-to-dock" | "servers" | "roster" | "character-operation";
		operation?: import("@/engine/contracts/session").CharacterOperationResult;
		action?: number;
		expectedCharacter?: string;
		apiBase: string;
	} | null = null;
	/*
================
mintWorldToken
================
	*/
	function mintWorldToken(
		kind: "transport" | "enterworld",
		character: string,
		signal: AbortSignal
	): Promise<string> {
		if ( !identity ) throw new Error( "Authentication required" );
		return http.mint( identity.apiBase, identity.token, kind, character, identity.divisionId, signal ).then(
			result => {
				const value = result.body as { ok?: boolean; token?: string; };
				if (
					!result.httpOk || value?.ok !== true || typeof value.token !== "string" || !value.token ||
					value.token.length > 512
				) throw new Error( "Admission rejected" );
				return value.token;
			}
		);
	}
	const incidentQueue: Array<
		{ body: ClientIncident; apiBase: string; token: string; attempts: number; next: number; }
	> = [];
	let incidentSending = false;
	const incidentDelivery = new Map<string, "pending" | "sent" | "failed">();
	/*
================
reportIncident

Bounded retries retain the reporting identity. Reports never block gameplay.
================
	*/
	function reportIncident( incident: ClientIncident ) {
		if ( !identity ) return;
		if ( incidentQueue.length >= INCIDENT_QUEUE_LIMIT ) return;
		const build = new URL( import.meta.url ).pathname.split( "/" ).at( -1 )?.slice( 0, 64 ) ?? "development";
		incidentQueue.push( {
			body: { ...incident, build },
			apiBase: identity.apiBase,
			token: identity.token,
			attempts: 0,
			next: 0
		} );
		if ( incident.id ) incidentDelivery.set( incident.id, "pending" );
		flushIncidents();
	}
	/*
================
flushIncidents
================
	*/
	function flushIncidents() {
		const report = incidentQueue[0];
		if ( disposed || incidentSending || !report || report.next > performance.now() ) return;
		incidentSending = true;
		report.attempts++;
		http.incident( report.apiBase, report.token, report.body, AbortSignal.timeout( INCIDENT_TIMEOUT_MS ) ).then(
			result => {
				const receipt = result.body as { ok?: boolean; id?: string; };
				return result.httpOk && receipt?.ok === true && receipt.id === report.body.id;
			},
			() => false
		).then( delivered => {
			incidentSending = false;
			if ( disposed ) return;
			if ( delivered || report.attempts >= INCIDENT_MAX_ATTEMPTS ) {
				incidentQueue.shift();
				if ( report.body.id ) incidentDelivery.set( report.body.id, delivered ? "sent" : "failed" );
				if ( incidentDelivery.size > 32 ) incidentDelivery.delete( incidentDelivery.keys().next().value! );
			} else report.next = performance.now() + INCIDENT_RETRY_MS * 2 ** (report.attempts - 1);
			if ( scope === "world" ) publishWorld();
			else if ( state.incidentID === report.body.id ) {
				publish( { ...state, incidentDelivery: incidentDelivery.get( report.body.id! ) } );
			}
		} );
	}
	const world = createWorldSession( mintWorldToken, http.references, reportIncident );
	let worldRevision = 0;
	// Only the active lifecycle may publish session state. Request generations
	// protect replacement within title; this scope protects transitions out of it.
	let scope: "title" | "world" = "title";
	/*
================
publish

The one state writer. Once a server has answered that it speaks another
release protocol, every state says so and names the remedy.
================
	*/
	function publish( next: Omit<SessionState, "revision"> ) {
		const outdated = http.releaseOutdated();
		state = Object.freeze( {
			...next,
			nativeServerName: identity?.nativeServerName,
			...(outdated ? { releaseOutdated: true, error: RELEASE_OUTDATED_MESSAGE } : {}),
			revision: state.revision + 1
		} );
		dirty = true;
	}
	/*
================
cancelTitleRequest
================
	*/
	function cancelTitleRequest() {
		generation++;
		controller?.abort();
		controller = null;
		completion = null;
		logoutPending = false;
		forgetStartingRetry();
	}
	/*
================
forgetStartingRetry

The repeat of a request answered "starting" holds the player's login
command, password included; it lives only until that request settles.
The wait's start survives the repeat itself, so the cap still applies.
================
	*/
	function forgetStartingRetry() {
		lastTitleCommand = undefined;
		startingRetry = null;
		if ( !repeatingStart ) startingSince = undefined;
	}
	/*
================
publishWorld
================
	*/
	function publishWorld() {
		if ( scope !== "world" ) return;
		const value = world.status();
		worldRevision = value.revision;
		if ( value.phase === "disconnected" && !value.admitted && identity ) {
			scope = "title";
			restoringWorld = false;
			publish( {
				...state,
				phase: "character-select",
				restoringWorld: false,
				character: undefined,
				error: value.error,
				disconnectMessage: value.disconnectMessage,
				incidentID: value.incidentID,
				incidentDelivery: incidentDelivery.get( value.incidentID )
			} );
			return;
		}
		publish( {
			restoringWorld,
			crestPrefix,
			marksBase: identity?.apiBase,
			phase: value.phase,
			character: value.character,
			entityCount: value.entities,
			pingMs: value.pingMs,
			error: value.error,
			disconnectMessage: value.disconnectMessage,
			incidentID: value.incidentID,
			incidentDelivery: incidentDelivery.get( value.incidentID ),
			divisionId: identity?.divisionId,
			characters: state.characters
		} );
	}
	/*
================
reset
================
	*/
	function reset() {
		cancelTitleRequest();
		resumeCharacter = undefined;
		restoringWorld = false;
		scope = "title";
		world.disconnect( true );
		worldRevision = world.status().revision;
		identity = null;
	}
	/*
================
baseUrl
================
	*/
	function baseUrl( value: string, base?: string ) {
		const url = new URL( value, base );
		if (
			![ "http:", "https:" ].includes( url.protocol ) || url.username || url.password || url.search || url.hash
		) {
			throw new Error( "Invalid session endpoint" );
		}
		return url.toString().replace( /\/$/, "" );
	}
	const session: SessionOwner = {
		isWorldReady: () => world.status().phase === "world" && world.status().ready,
		/*
		================
		command
		================
		*/
		command( command, now ) {
			if ( disposed ) {
				throw new Error( "Session disposed" );
			}
			if ( command.kind === "chat-blocks" ) {
				world.chatBlocks( command.value );
				return;
			}
			if ( command.kind === "game-options" ) {
				world.options( command.value );
				return;
			}
			if ( command.kind === "cancel-character-operation" ) {
				if ( scope === "title" && state.characterOperation?.status === "pending" ) {
					cancelTitleRequest();
					publish( { ...state, characterOperation: undefined } );
				}
				return;
			}
			if (
				command.kind === "check-name" || command.kind === "create-character" ||
				command.kind === "delete-character" || command.kind === "restore-character"
			) {
				if ( scope !== "title" || !identity ) {
					publish( { ...state, error: "Character operation requires authenticated title" } );
					return;
				}
				if ( state.characterOperation?.status === "pending" ) {
					publish( { ...state, error: "Character operation pending" } );
					return;
				}
				if ( !Number.isSafeInteger( command.operationId ) || command.operationId < 1 ) {
					publish( { ...state, error: "Invalid character operation identity" } );
					return;
				}
				cancelTitleRequest();
				controller = new AbortController();
				const current = generation;
				const operation = { operationId: command.operationId, kind: command.kind, status: "pending" as const };
				const action = command.kind === "check-name" ?
					4 :
					command.kind === "create-character" ?
					1 :
					command.kind === "delete-character" ?
					3 :
					5;
				const route = action === 4 ?
					"/character/name-overlap" :
					action === 1 ?
					"/character/create" :
					"/character/delete-action";
				const body = command.kind === "create-character" ?
					{ ...command.draft, divisionId: identity.divisionId } :
					{
						characterName: command.characterName,
						divisionId: identity.divisionId,
						...(action === 3 || action === 5 ? { action } : {})
					};
				publish( { ...state, error: undefined, characterOperation: operation } );
				http.character( identity.apiBase, identity.token, route, body, controller.signal ).then( value => {
					if ( !disposed && current === generation ) {
						completion = {
							generation: current,
							value,
							expectedShard: identity?.divisionId ?? "",
							kind: "character-operation",
							apiBase: identity?.apiBase ?? "",
							operation,
							action,
							expectedCharacter: body.characterName
						};
					}
				}, error => {
					if ( !disposed && current === generation ) {
						completion = {
							generation: current,
							error: String( error ),
							expectedShard: "",
							kind: "character-operation",
							apiBase: "",
							operation,
							action
						};
					}
				} );
				return;
			}
			if (
				command.kind === "enter-world" || command.kind === "disconnect" || command.kind === "restart" ||
				command.kind === "exit" || command.kind === "reconnect" || command.kind === "world-ready"
			) {
				try {
					if ( command.kind === "enter-world" ) {
						if ( !identity ) throw new Error( "Authentication required" );
						world.enter( command.character, identity.divisionId, identity.transportUrl );
						cancelTitleRequest();
						scope = "world";
					} else {
						if ( scope !== "world" ) throw new Error( "No active world session" );
						if ( command.kind === "disconnect" ) world.disconnect();
						else if ( command.kind === "restart" || command.kind === "exit" ) {
							world.depart( command.kind === "restart" ? 2 : 1 );
						} else if ( command.kind === "reconnect" ) world.reconnect();
						else world.ready( command.travelRevision );
					}
					publishWorld();
				} catch ( error ) {
					publish( { ...state, error: String( error ) } );
				}
				return;
			}
			if ( command.kind === "gameplay" ) {
				try {
					world.command( command.command, now );
				} catch ( error ) {
					publish( { ...state, error: String( error ) } );
				}
				return;
			}
			if ( command.kind === "servers" || command.kind === "roster" ) {
				if ( scope !== "title" ) {
					publish( { ...state, error: "Leave the world session before requesting title data" } );
					return;
				}
				if ( command.kind === "roster" && !identity ) {
					publish( { phase: "failed", error: "Authentication required" } );
					return;
				}
				if ( command.kind === "servers" && identity ) {
					publish( { phase: "failed", error: "Log out before selecting another server" } );
					return;
				}
				cancelTitleRequest();
				if ( command.kind === "servers" ) lastTitleCommand = command;
				controller = new AbortController();
				const current = generation, kind = command.kind;
				try {
					const base = kind === "servers" ?
						baseUrl(
							(command as {
								apiBase: string;
							}).apiBase
						) :
						identity!.apiBase;
					publish( {
						phase: kind === "servers" ? "listing-servers" : "loading-roster",
						...(identity ? { divisionId: identity.divisionId } : {})
					} );
					browserBase = base;
					if ( kind === "servers" && !restoreAttempted ) {
						restoreAttempted = true;
						const signal = controller.signal;
						http.restore( base, signal ).then( value => {
							if ( disposed || current !== generation ) return;
							const body = value.body as { ok?: boolean; divisionId?: unknown; } | null;
							if ( value.httpOk && body?.ok === true ) {
								completion = {
									generation: current,
									value,
									expectedShard: typeof body.divisionId === "string" ? body.divisionId : "",
									kind: "restore",
									apiBase: base
								};
								return;
							}
							return http.servers( base, signal ).then( listed => {
								if ( !disposed && current === generation ) {
									completion = {
										generation: current,
										value: listed,
										expectedShard: "",
										kind: "servers",
										apiBase: base
									};
								}
							} );
						} ).catch( () => {
							if ( !disposed && current === generation ) {
								completion = {
									generation: current,
									error: "Session restoration failed",
									expectedShard: "",
									kind: "servers",
									apiBase: base
								};
							}
						} );
						return;
					}
					const operation = kind === "servers" ?
						http.servers( base, controller.signal ) :
						http.roster( base, identity!.token, controller.signal );
					operation.then( value => {
						if ( !disposed && current === generation ) {
							completion = {
								generation: current,
								value,
								expectedShard: identity?.divisionId ?? "",
								kind,
								apiBase: base
							};
						}
					}, () => {
						if ( !disposed && current === generation ) {
							completion = {
								generation: current,
								error: "Session request failed",
								expectedShard: "",
								kind,
								apiBase: base
							};
						}
					} );
				} catch {
					controller.abort();
					controller = null;
					publish( { phase: "failed", error: "Invalid session request" } );
				}
				return;
			}
			if ( command.kind === "logout" && logoutPending ) return;
			const selectedServer = command.kind === "login" ?
				state.servers?.find( s => s.id === command.serverId ) :
				undefined;
			reset();
			if ( command.kind === "login" ) lastTitleCommand = command;
			crestPrefix = selectedServer?.nativeServerId;
			if ( command.kind === "logout" ) {
				restoreAttempted = true;
				if ( !browserBase ) {
					publish( { phase: "signed-out" } );
					return;
				}
				controller = new AbortController();
				logoutPending = true;
				const current = generation, base = browserBase;
				publish( { phase: "authenticating" } );
				http.logout( base, controller.signal ).then( value => {
					if ( !disposed && current === generation ) {
						completion = { generation: current, value, expectedShard: "", kind: "logout", apiBase: base };
					}
				}, () => {
					if ( !disposed && current === generation ) {
						completion = {
							generation: current,
							error: "Sign out failed; please retry",
							expectedShard: "",
							kind: "logout",
							apiBase: base
						};
					}
				} );
				return;
			}
			try {
				const base = baseUrl( command.apiBase );
				browserBase = base;
				restoreAttempted = true;
				if (
					!command.id || !command.password || !command.serverId || command.id.length > 256 ||
					command.password.length > 1024
				) {
					throw new Error( "Invalid login input" );
				}
				const current = generation;
				controller = new AbortController();
				publish( { phase: "authenticating" } );
				http.login( base, {
					id: command.id,
					password: command.password,
					serverId: command.serverId,
					...(command.divisionId ? { divisionId: command.divisionId } : {})
				}, controller.signal ).then( value => {
					if ( !disposed && current === generation ) {
						completion = {
							generation: current,
							value,
							expectedShard: command.serverId,
							kind: "login",
							apiBase: base
						};
					}
				}, () => {
					if ( !disposed && current === generation ) {
						completion = {
							generation: current,
							error: "Authentication request failed",
							expectedShard: command.serverId,
							kind: "login",
							apiBase: base
						};
					}
				} );
			} catch {
				publish( { phase: "failed", error: "Invalid login request" } );
			}
		},
		step( now = performance.now() ) {
			if ( disposed ) {
				return null;
			}
			world.step( now );
			flushIncidents();
			if ( startingRetry && now >= startingRetry.dueMs && scope === "title" ) {
				const repeat = startingRetry.command;
				startingRetry = null;
				repeatingStart = true;
				try {
					session.command( repeat, now );
				} finally {
					repeatingStart = false;
				}
			}
			const departure = world.takeDeparture();
			if ( departure && identity ) {
				cancelTitleRequest();
				scope = "title";
				restoringWorld = false;
				resumeCharacter = undefined;
				const base = identity.apiBase, current = generation;
				controller = new AbortController();
				if ( departure === 1 ) {
					identity = null;
					logoutPending = true;
					publish( { phase: "authenticating" } );
				} else {publish( {
						...state,
						phase: "character-select",
						character: undefined,
						restoringWorld: false,
						entityCount: 0,
						error: undefined
					} );}
				const kind = departure === 1 ? "logout" : "return-to-dock";
				(departure === 1 ?
					http.logout( base, controller.signal ) :
					http.returnToDock( base, controller.signal )).then( value => {
						if ( !disposed && generation === current ) {
							completion = { generation: current, value, kind, expectedShard: "", apiBase: base };
						}
					}, () => {
						if ( !disposed && generation === current ) {
							completion = {
								generation: current,
								error: "Session navigation failed",
								kind,
								expectedShard: "",
								apiBase: base
							};
						}
					} );
			}
			const worldState = world.status();
			if ( scope === "world" && worldState.revision !== worldRevision ) publishWorld();
			if ( completion ) {
				const result = completion;
				completion = null;
				controller = null;
				if ( scope === "title" && result.generation === generation ) {
					try {
						// The server is still starting: keep the waiting state and
						// repeat the request after its Retry-After, up to the cap.
						const starting = result.value && !result.value.httpOk &&
								(result.kind === "servers" || result.kind === "login") ?
							serverStarting( result.value.body ) :
							null;
						if ( starting && lastTitleCommand ) {
							startingSince ??= now;
							if ( now - startingSince < MAX_STARTING_WAIT_MS ) {
								startingRetry = { dueMs: now + starting.retryMs, command: lastTitleCommand };
								const waiting = dirty ? state : null;
								dirty = false;
								return waiting;
							}
							forgetStartingRetry();
							publish( { phase: "failed", code: STARTING_CODE, error: starting.message } );
							dirty = false;
							return state;
						}
						forgetStartingRetry();
						if ( result.kind === "return-to-dock" ) {
							if (
								result.error || !result.value?.httpOk ||
								(result.value.body as { ok?: boolean; })?.ok !== true
							) publish( { ...state, error: "Could not save character-select navigation" } );
							else {
								// Refresh equipment/levels after play through the authoritative roster reader.
								controller = new AbortController();
								const current = generation;
								http.roster( identity!.apiBase, identity!.token, controller.signal ).then( value => {
									if ( !disposed && generation === current ) {
										completion = {
											generation: current,
											value,
											kind: "roster",
											expectedShard: identity!.divisionId,
											apiBase: identity!.apiBase
										};
									}
								}, () => {
									if ( !disposed && generation === current ) {
										completion = {
											generation: current,
											error: "Roster refresh failed",
											kind: "roster",
											expectedShard: "",
											apiBase: ""
										};
									}
								} );
							}
							const resultState = dirty ? state : null;
							dirty = false;
							return resultState;
						}
						if ( result.kind === "logout" ) {
							logoutPending = false;
							if (
								result.error || !result.value?.httpOk ||
								(result.value.body as { ok?: boolean; })?.ok !== true
							) publish( { phase: "failed", error: result.error ?? "Sign out failed; please retry" } );
							else publish( { phase: "signed-out" } );
							dirty = false;
							return state;
						}
						if ( result.kind === "character-operation" ) {
							const body = result.value?.body as {
								action?: number;
								nativeResult?: number;
								nativeErrorCode?: number;
								character?: unknown;
								characterRosterContractVersion?: number;
							} | undefined;
							if (
								result.error || !result.value?.httpOk || !body || body.action !== result.action ||
								body.nativeResult !== 1
							) {
								publish( {
									...state,
									characterOperation: {
										...result.operation!,
										status: "failed",
										nativeErrorCode: body?.nativeErrorCode,
										error: result.error ?? "Character request rejected"
									}
								} );
							} else {
								let characters = state.characters;
								if ( result.action !== 4 ) {
									const [character] = decoder.roster( {
										action: 2,
										nativeResult: 1,
										characterRosterContractVersion: body.characterRosterContractVersion,
										characters: [ body.character ]
									} );
									const index = (characters ?? []).findIndex( c => c.id === character!.id );
									if ( character!.name !== result.expectedCharacter ) {
										throw Error( "Character operation returned a different character" );
									}
									if ( result.action === 1 && index >= 0 ) {
										throw Error( "Creation returned an existing roster identity" );
									}
									if (
										result.action !== 1 &&
										(index < 0 || characters![index]!.name !== result.expectedCharacter)
									) throw Error( "Character operation returned an unknown roster identity" );
									characters = index < 0 ?
										[ ...(characters ?? []), character! ] :
										characters!.map( ( row, i ) => i === index ? character! : row );
								}
								publish( {
									...state,
									characters,
									characterOperation: { ...result.operation!, status: "succeeded" }
								} );
							}
							dirty = false;
							return state;
						}
						if ( result.error ) {
							throw new Error( result.error );
						}
						if ( result.kind !== "login" && result.kind !== "restore" ) {
							if ( !result.value?.httpOk ) {
								throw new Error( "Session HTTP request rejected" );
							}
							if ( result.kind === "servers" ) {
								publish( { phase: "signed-out", servers: decoder.servers( result.value.body ) } );
							} else {
								const characters = decoder.roster( result.value.body ),
									resume = characters.find( row =>
										row.name === resumeCharacter && !row.deletePending
									);
								resumeCharacter = undefined;
								publish( {
									phase: "character-select",
									crestPrefix,
									divisionId: identity!.divisionId,
									characters
								} );
								if ( resume ) {
									world.enter( resume.name, identity!.divisionId, identity!.transportUrl );
									cancelTitleRequest();
									scope = "world";
									restoringWorld = true;
									publishWorld();
								}
							}
							dirty = false;
							return state;
						}
						if ( !result.value || !result.value.body || typeof result.value.body !== "object" ) {
							throw new Error( "Invalid authentication response" );
						}
						const value = result.value.body as Record<string, unknown>;
						if (
							value.ok === false && typeof value.code === "string" && typeof value.message === "string"
						) {
							if (
								value.nativeTitleArgument !== undefined &&
								(typeof value.nativeTitleArgument !== "number" ||
									!Number.isInteger( value.nativeTitleArgument ) || value.nativeTitleArgument < 0 ||
									value.nativeTitleArgument > 0xffffffff)
							) throw Error( "Invalid native title argument" );
							publish( {
								phase: "failed",
								code: value.code,
								error: value.message,
								...(typeof value.nativeTitleStatus === "number" &&
										Number.isInteger( value.nativeTitleStatus ) ?
									{
										nativeTitleStatus: value.nativeTitleStatus,
										nativeTitleArgument: value.nativeTitleArgument as number | undefined
									} :
									{})
							} );
							if ( !dirty ) {
								return null;
							}
							dirty = false;
							return state;
						}
						if ( !result.value.httpOk || value.ok !== true ) {
							throw new Error( "Invalid authentication response" );
						}
						if (
							typeof value.sessionToken !== "string" || !value.sessionToken.trim() ||
							value.sessionToken.length > 4096 || !result.expectedShard ||
							value.divisionId !== result.expectedShard || typeof value.transportUrl !== "string" ||
							value.nextScene !== "character-select"
						) {
							throw new Error( "Invalid authentication route" );
						}
						if ( value.nativeServerId !== undefined ) {
							if (
								typeof value.nativeServerId !== "number" || !Number.isInteger( value.nativeServerId ) ||
								value.nativeServerId < 1 || value.nativeServerId > 65535
							) throw Error( "Invalid crest server prefix" );
							crestPrefix = value.nativeServerId;
						}
						// Agent advertises an edge route ("/shards/<id>") or a public host; resolving it
						// against the API URL keeps routed transports on whatever origin served the client.
						if (
							value.nativeServerName !== undefined &&
							(typeof value.nativeServerName !== "string" || value.nativeServerName.length > 4096)
						) throw Error( "Invalid native shard name" );
						identity = {
							nativeServerName: value.nativeServerName as string | undefined,
							apiBase: result.apiBase,
							token: value.sessionToken,
							divisionId: result.expectedShard,
							transportUrl: baseUrl( value.transportUrl, result.apiBase )
						};
						if (
							result.kind === "restore" && typeof value.resumeCharacter === "string" &&
							value.resumeCharacter.length > 0 && value.resumeCharacter.length <= 64
						) {
							resumeCharacter = value.resumeCharacter;
							controller = new AbortController();
							const current = generation;
							publish( { phase: "loading-roster", divisionId: identity.divisionId } );
							http.roster( identity.apiBase, identity.token, controller.signal ).then( value => {
								if ( !disposed && current === generation ) {
									completion = {
										generation: current,
										value,
										kind: "roster",
										expectedShard: result.expectedShard,
										apiBase: result.apiBase
									};
								}
							}, () => {
								if ( !disposed && current === generation ) {
									completion = {
										generation: current,
										error: "Character restoration failed",
										kind: "roster",
										expectedShard: result.expectedShard,
										apiBase: result.apiBase
									};
								}
							} );
						} else publish( { phase: "character-select", crestPrefix, divisionId: identity.divisionId } );
					} catch ( error ) {
						if ( result.kind === "character-operation" ) {
							publish( {
								...state,
								characterOperation: { ...result.operation!, status: "failed", error: String( error ) }
							} );
							dirty = false;
							return state;
						}
						if ( result.kind === "login" ) identity = null;
						publish( { phase: "failed", error: String( error ) } );
					}
				}
			}
			if ( !dirty ) {
				return null;
			}
			dirty = false;
			return state;
		},
		takeWorld: () => world.take(),
		ackWorld: sequence => world.ack( sequence ),
		dispose() {
			if ( !disposed ) {
				disposed = true;
				// Teardown has no presentation consumer. Do not enqueue a logout
				// reset into a possibly full journal and mask the original failure.
				cancelTitleRequest();
				identity = null;
				scope = "title";
				world.dispose();
				dirty = false;
			}
		}
	};
	return session;
}
