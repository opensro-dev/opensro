/*
===========================================================================

item-mall.ts - native mall navigation and confirmation lifetime

Owns browsing choices only. The gameplay publication owns catalogue, balances
and purchase completion. Closing a dialog never retries a purchase.

===========================================================================
*/

import type { MallGender, MallOffer, MallState, MallPreviewState } from "@/engine/contracts/item-mall";
import {
	mallCategories,
	mallCategoriesLater,
	MALL_PAGE_SIZE_LIMIT,
	MALL_ROWS_PER_PAGE
} from "@/engine/foundation/ui/item-mall-layout";
import { renewalGoodsCells } from "@/engine/foundation/ui/renewal-mall";
import type { MallPurchase } from "@/engine/foundation/gameplay/item-mall-wire";
import { MAGIC_CUBE_COPY } from "@/engine/foundation/gameplay/magic-cube";

/*
================
createItemMall
================
*/
export function createItemMall() {
	let visible = false;
	let category = -1;
	let tab = 0;
	let page = 0;
	// The first sub tab the renewal window's scrolled tab row draws
	// (issue #4093); the view clamps it to the category's tabs.
	let tabFirst = 0;
	// Port-only (issue #3257): the later category rail is in force only
	// while the Experimental mall categories row is on, and the item
	// search text the rail's search field edits. Native browsing never
	// touches either.
	let categoriesLater = false;
	let search = "";
	// The renewal mall's gender filter (issue #4480, port-only): the
	// chosen wearer sex, matched against each package's own gender (its
	// items' ReqGender, published by the server). Unlike the search it
	// survives category switches; reset() restores "all".
	let gender: "all" | MallGender = "all";
	// The later client's renewal mall window, the desktop default when
	// its import is published (owner 2026-11-20). It browses the later
	// rail, opens a popup per goods card (popup is its index on the page,
	// -1 none) and its main page lists the badged goods. Off is the
	// classic window.
	let renewal = false;
	let popup = -1;
	// The renewal window's search field edits a draft; its Search button
	// (or Enter) applies it as the search above.
	let searchDraft = "";
	// The client's text table: offer names are text keys, and a search
	// matches what the player reads, never the key.
	let names: Readonly<Record<string, string>> = {};
	let pageSize = MALL_ROWS_PER_PAGE;
	let selected: MallOffer | null = null;
	let quantity = 1;
	let points = 0;
	let revision = 0;
	let pointDialog = false;
	let pointDraft = 0;
	let bagPage = 0;
	let compactView: "shop" | "bag" | "preview" = "shop";
	let compactBagPage = 0;
	const basket = new Set<number>();
	const worn = new Map<number, MallOffer>();
	let preview: MallPreviewState = { wearable: [] };
	let question: { kind: "reserve" | "remove" | "worn" | "basket"; offers: readonly MallOffer[]; } | null = null;
	let queue: readonly (MallOffer & MallPurchase)[] = [];
	let awaiting: { revision: number; offer: MallOffer; } | null = null;
	// The Buying list page (native CIFSilkBuyList): open, the period combo's
	// selection (0..3) and whether its dropdown is open. The rows themselves
	// are the worker's MallSilkHistoryState publication, never local state.
	let silkOpen = false;
	let silkPeriod = 0;
	let silkComboOpen = false;

	/*
	================
	currentOffer
	================
	*/
	function currentOffer( offer: MallOffer, state: MallState ) {
		return state.offers.find( current =>
			current.packageId === offer.packageId &&
			current.group === offer.group && current.shop === offer.shop && current.tab === offer.tab &&
			current.slot === offer.slot && current.silk === offer.silk && current.giftSilk === offer.giftSilk &&
			current.allowsPoints === offer.allowsPoints
		);
	}

	/*
	================
	pointLimit
	================
	*/
	function pointLimit() {
		if ( selected ) return selected.allowsPoints ? selected.silk * quantity : 0;
		return question?.offers.reduce( ( sum, offer ) => sum + (offer.allowsPoints ? offer.silk : 0), 0 ) ?? 0;
	}

	/*
	================
	questionReady

	Validate the whole quote before sending its first element. Every subsequent
	purchase is also checked against its fresh authoritative balance.
	================
	*/
	function questionReady( state: MallState ) {
		if ( !question || state.pending || awaiting || queue.length ) return false;
		if ( question.kind === "reserve" || question.kind === "remove" ) return true;
		if ( question.offers.some( offer => !currentOffer( offer, state ) ) ) return false;
		const silk = question.offers.reduce( ( sum, offer ) => sum + offer.silk, 0 );
		const gift = question.offers.reduce( ( sum, offer ) => sum + offer.giftSilk, 0 );
		return points <= state.points && points <= pointLimit() && silk - points <= state.silk + state.earnedSilk &&
			gift <= state.giftSilk;
	}

	/*
	================
	reset
	================
	*/
	/*
	================
	categories

	The rail list in force: the later one only while its Experimental
	row is on.
	================
	*/
	function categories() {
		return categoriesLater || renewal ? mallCategoriesLater() : mallCategories();
	}

	function reset() {
		visible = false;
		gender = "all";
		category = -1;
		tab = 0;
		tabFirst = 0;
		page = 0;
		categoriesLater = false;
		search = "";
		searchDraft = "";
		renewal = false;
		popup = -1;
		pageSize = MALL_ROWS_PER_PAGE;
		selected = null;
		pointDialog = false;
		quantity = 1;
		points = 0;
		pointDraft = 0;
		bagPage = 0;
		compactView = "shop";
		compactBagPage = 0;
		silkOpen = false;
		silkPeriod = 0;
		silkComboOpen = false;
		revision = 0;
		basket.clear();
		worn.clear();
		preview = { wearable: [] };
		question = null;
		queue = [];
		awaiting = null;
	}

	return {
		/*
		================
		open
		================
		*/
		open() {
			visible = true;
		},
		/*
		================
		close
		================
		*/
		close() {
			visible = false;
			compactView = "shop";
			selected = null;
			popup = -1;
			pointDialog = false;
			silkOpen = false;
			silkComboOpen = false;
			worn.clear();
			question = null;
			queue = [];
		},
		/*
		================
		observe
		================
		*/
		observe( state: MallState | undefined ) {
			if ( !state || state.revision === revision ) return false;
			revision = state.revision;
			if ( awaiting && state.revision !== awaiting.revision && !state.pending ) {
				if ( state.error ) queue = [];
				else {
					basket.delete( awaiting.offer.packageId );
					queue = queue.slice( 1 );
				}
				awaiting = null;
			}
			if ( !state.pending && !state.error ) selected = null;
			pointDialog = false;
			return true;
		},
		/*
		================
		browse
		================
		*/
		browse( index: number, nextTab = 0 ) {
			if ( !Number.isInteger( index ) || index < -1 || index >= categories().length ) return;
			if ( !Number.isInteger( nextTab ) || nextTab < 0 ) return;
			// A tab of the open category keeps the row where it stands.
			if ( index !== category ) tabFirst = 0;
			category = index;
			compactView = "shop";
			tab = nextTab;
			page = 0;
			search = "";
			searchDraft = "";
			selected = null;
			popup = -1;
			pointDialog = false;
			silkOpen = false;
			silkComboOpen = false;
		},
		/*
		================
		setCategoriesLater

		The Experimental mall categories row, as the renderer reads it
		each frame. A switch restarts browsing at the rail: the two
		rails' indices name different categories, and a search or a
		tab from the other rail would point at nothing (issue #3257).
		================
		*/
		setCategoriesLater( on: boolean ) {
			if ( on === categoriesLater ) return;
			const before = categories();
			categoriesLater = on;
			// Under the renewal window both settings name the later rail.
			if ( categories() === before ) return;
			category = -1;
			tab = 0;
			tabFirst = 0;
			page = 0;
			search = "";
			searchDraft = "";
			selected = null;
			popup = -1;
		},
		/*
		================
		setRenewal

		Whether the renewal window is the one drawn, as the renderer decides
		it each frame. The renewal window always browses the later rail, so a
		switch restarts browsing at its main page, as a rail switch does.
		================
		*/
		setRenewal( on: boolean ) {
			if ( on === renewal ) return;
			renewal = on;
			category = -1;
			tab = 0;
			tabFirst = 0;
			page = 0;
			search = "";
			searchDraft = "";
			selected = null;
			popup = -1;
		},
		/*
		================
		openPopup

		A renewal goods card's popup (buy, preview, basket); index is the
		card's place on the current page. A second click on the same card
		closes it.
		================
		*/
		openPopup( index: number ) {
			if ( !Number.isInteger( index ) || index < 0 || index >= pageSize ) return;
			popup = popup === index ? -1 : index;
		},
		/*
		================
		closePopup
		================
		*/
		closePopup() {
			popup = -1;
		},
		/*
		================
		setSearch

		The item search text: at most 32 characters (the field's own
		cap), and any edit returns to the first page.
		================
		*/
		setSearch( text: string ) {
			if ( typeof text !== "string" ) return;
			search = text.slice( 0, 32 );
			page = 0;
			popup = -1;
		},
		/*
		================
		setGender

		The renewal mall's gender filter (issue #4480): "all", "male" or
		"female". A change returns to the first page, as a search edit
		does.
		================
		*/
		setGender( value: string ) {
			if ( value !== "all" && value !== "male" && value !== "female" ) return;
			gender = value;
			page = 0;
			popup = -1;
		},
		/*
		================
		setNames

		The client's text table, as the renderer holds it; search and the
		renewal grid's pairing read offer names through it.
		================
		*/
		setNames( table: Readonly<Record<string, string>> ) {
			names = table;
		},
		/*
		================
		setSearchDraft

		The renewal search field's text, at most 32 characters; the list
		changes only when applySearch takes it.
		================
		*/
		setSearchDraft( text: string ) {
			if ( typeof text !== "string" ) return;
			searchDraft = text.slice( 0, 32 );
		},
		/*
		================
		applySearch

		The renewal Search button (and Enter in its field): the draft
		becomes the search, from the first page. An empty draft clears the
		search and returns the list to the category (or the main page).
		================
		*/
		applySearch() {
			search = searchDraft.trim();
			page = 0;
			popup = -1;
		},
		/*
		================
		openSilk

		The frame's Buying List button (child 6). The native button is born
		disabled (0x6BC17E); the port enables it only behind the Experimental
		silk history option, and the renderer owns that gate - opening the
		page here is unconditional so the state stays testable.
		================
		*/
		openSilk() {
			silkOpen = true;
			silkComboOpen = false;
		},
		/*
		================
		selectSilkPeriod
		================
		*/
		selectSilkPeriod( index: number ) {
			if ( !Number.isInteger( index ) || index < 0 || index > 3 ) return;
			silkPeriod = index;
			silkComboOpen = false;
		},
		/*
		================
		toggleSilkCombo
		================
		*/
		toggleSilkCombo() {
			silkComboOpen = !silkComboOpen;
		},
		/*
		================
		scrollTabs

		Starts the renewal window's scrolled tab row at first (issue
		#4093). The selected tab stays selected wherever the row stands.
		================
		*/
		scrollTabs( first: number ) {
			if ( !Number.isInteger( first ) || first < 0 ) return;
			tabFirst = first;
		},
		/*
		================
		paginate
		================
		*/
		paginate( next: number, count: number ) {
			if ( !Number.isInteger( next ) || !Number.isInteger( count ) || count < 0 ) return;
			page = Math.max( 0, Math.min( Math.ceil( count / pageSize ) - 1, next ) );
			popup = -1;
		},
		/*
		================
		setPageSize

		Presentation only: keep the first visible offer on rotation. Action indices
		continue to address read().offers, including purchases and basket changes.
		The classic window shows six rows; the renewal grid shows ten cards.
		================
		*/
		setPageSize( count: number ) {
			if ( !Number.isInteger( count ) || count < 1 || count > MALL_PAGE_SIZE_LIMIT ) return;
			if ( count === pageSize ) return;
			page = Math.floor( page * pageSize / count );
			popup = -1;
			pageSize = count;
		},
		/*
		================
		compactAction

		Port-only secondary-view navigation. Never touches purchase state or
		server commands; the ordinary catalogue action IDs retain their meaning.
		================
		*/
		compactAction( id: string ) {
			if ( id === "item-mall-view:bag" || id === "item-mall-view:preview" ) {
				compactView = id === "item-mall-view:bag" ? "bag" : "preview";
				return true;
			}
			if ( id.startsWith( "item-mall-bag-slice:" ) ) {
				const next = Number( id.slice( "item-mall-bag-slice:".length ) );
				if ( Number.isSafeInteger( next ) && next >= 0 ) compactBagPage = next;
				return true;
			}
			return false;
		},
		/*
		================
		choose
		================
		*/
		choose( offer: MallOffer ) {
			if ( awaiting || queue.length || question ) return;
			popup = -1;
			selected = offer;
			pointDialog = false;
			quantity = 1;
			points = 0;
		},
		/*
		================
		cancel
		================
		*/
		cancel() {
			selected = null;
			pointDialog = false;
		},
		/*
		================
		edit

		6C0100 caps ordinary package counts at five; single loose stackables use
		their native stack limit. The server supplies the resolved package limit.
		================
		*/
		edit( value: number, contribution: number, state: MallState ) {
			if (
				(!selected && !question) || state.pending || !Number.isSafeInteger( value ) ||
				!Number.isSafeInteger( contribution )
			) {
				return;
			}
			quantity = selected ? Math.max( 1, Math.min( selected.purchaseLimit, value ) ) : 1;
			points = Math.max( 0, Math.min( contribution, state.points, pointLimit() ) );
		},
		/*
		================
		reserve
		================
		*/
		reserve( offer: MallOffer ) {
			if ( basket.has( offer.packageId ) ) basket.delete( offer.packageId );
			else basket.add( offer.packageId );
		},
		/*
		================
		purchase
		================
		*/
		purchase( state: MallState ): MallPurchase | null {
			if ( !selected || state.pending || awaiting || queue.length || question ) return null;
			const current = state.offers.find( offer =>
				offer.packageId === selected?.packageId &&
				offer.group === selected.group && offer.shop === selected.shop &&
				offer.tab === selected.tab && offer.slot === selected.slot
			);
			if (
				!current || current.silk !== selected.silk || current.giftSilk !== selected.giftSilk ||
				quantity > current.purchaseLimit || points > state.points || points > current.silk * quantity ||
				points !== 0 && !current.allowsPoints
			) return null;
			if (
				selected.silk * quantity - points > state.silk + state.earnedSilk ||
				selected.giftSilk * quantity > state.giftSilk
			) {
				return null;
			}
			return { ...selected, quantity, points };
		},
		/*
		================
		isOpen

		Whether the mall window is open. The frame's gates (auto-potions,
		the quick-slot bar, travel) ask only this; read() builds the whole
		window model, offer lists included, and costs a kilobyte a frame
		when asked for one flag.
		================
		*/
		isOpen(): boolean {
			return visible;
		},
		/*
		================
		read
		================
		*/
		read( state?: MallState ) {
			const list = categories();
			const key = list[category]?.key;
			const tabs = state?.tabs.filter( entry => entry.category === key ) ?? [];
			// The Recommended tab is the later client's own (issue
			// #3257): it lists no tab of the catalogue's, it gathers
			// the offers badged "recommended" from every tab.
			const later = categoriesLater || renewal;
			const virtual = later && key === "MALL_HOT" && tabs.length > 0;
			const shown = virtual ?
				[ ...tabs, { shop: -1, tab: -1, category: "MALL_HOT", label: "UIIT_STT_SILKMALL_FAVORITE" } ] :
				tabs;
			const current = shown[tab];
			// The renewal main page (no category, no search) lists every
			// badged offer: the later mall's front page shows its new, hot
			// and recommended goods. INFERENCE: no v1.188 binary is at hand;
			// the badges are the catalogue's own (SRO_MALL_CATEGORIES).
			const front = renewal && category === -1 && search === "";
			const offers = state?.offers.filter( offer =>
				front ?
					offer.badge !== undefined :
					key === "basket" ?
					basket.has( offer.packageId ) :
					search !== "" ?
					(names[offer.name] || MAGIC_CUBE_COPY[offer.name] || offer.name).toLowerCase().includes(
						search.toLowerCase()
					) :
					current && current.shop < 0 ?
					offer.badge === "recommended" :
					current && offer.shop === current.shop && offer.tab === current.tab
			) ?? [];
			// The gender filter (#4480) narrows whatever list the tabs,
			// the search or the basket chose: a gendered package must
			// match the chosen sex, an ungendered one lists under every
			// choice. gendered says the list behind the filter holds a
			// gendered package at all (the control's reason to be).
			const gendered = offers.some( offer => offer.gender !== undefined );
			const listed = gender === "all" ?
				offers :
				offers.filter( offer => offer.gender === undefined || offer.gender === gender );
			// The renewal grid pages by its cells (gendered pairs keep their
			// rows, so a cell may stand empty); the classic window by offers.
			const cells = renewal ? renewalGoodsCells( listed, names ) : null;
			const total = cells ? cells.length : listed.length;
			const currentPage = Math.min( page, Math.max( 0, Math.ceil( total / pageSize ) - 1 ) );
			const pageCells = cells?.slice( currentPage * pageSize, (currentPage + 1) * pageSize );
			const pageOffers = pageCells ?
				pageCells.filter( index => index >= 0 ).map( index => listed[index]! ) :
				listed.slice( currentPage * pageSize, (currentPage + 1) * pageSize );
			// grid[cell] is the cell's index into the page's offers, or -1.
			let next = 0;
			const grid = pageCells ? pageCells.map( index => index >= 0 ? next++ : -1 ) : null;
			return {
				question,
				silk: { open: silkOpen, period: silkPeriod, comboOpen: silkComboOpen },
				batchPending: queue.length > 0 || awaiting !== null,
				questionReady: state ? questionReady( state ) : false,
				pointLimit: pointLimit(),
				previewGid: preview.gid,
				worn: [ ...worn.values() ],
				bagPage,
				compactView,
				compactBagPage,
				pointDialog,
				pointDraft,
				visible,
				categories: list,
				categoriesLater: later,
				renewal,
				popup,
				search,
				searchDraft,
				category,
				tab,
				tabFirst,
				page: currentPage,
				pageSize,
				selected,
				quantity,
				points,
				tabs: shown,
				count: total,
				offers: pageOffers,
				gender,
				gendered,
				grid
			};
		},
		/*
        ================
        showPoints
        ================
        */
		showPoints() {
			if ( pointLimit() > 0 ) {
				pointDraft = points;
				pointDialog = true;
			}
		},
		/*
        ================
        closePoints
        ================
        */
		closePoints() {
			pointDialog = false;
		},
		/*
        ================
        editPointDraft
        ================
        */
		editPointDraft( value: number, state: MallState ) {
			if ( !pointDialog || (!selected && !question) || state.pending || !Number.isSafeInteger( value ) ) return;
			pointDraft = Math.max( 0, Math.min( value, state.points, pointLimit() ) );
		},
		/*
		================
		selectBagPage
		================
		*/
		selectBagPage( value: number, pages: number ) {
			if ( !Number.isInteger( value ) || !Number.isInteger( pages ) || pages < 1 ) return;
			bagPage = Math.max( 0, Math.min( value, pages - 1 ) );
		},
		/*
		================
		canWear

		6C2390 enables native Wear only for the avatar dress and hat tabs.
		The shared visual catalogue also enforces the character's body family.
		================
		*/
		canWear( offer: MallOffer, state: MallState ) {
			return this.wearEnabled( offer, state ) && offer.itemIds.length > 0 &&
				offer.itemIds.every( id => preview.wearable.includes( id ) );
		},
		/*
		================
		wearEnabled

		6C2390 enables the control by tab. 6CA770 separately rejects an
		incompatible character body with the ordinary equipment error message.
		================
		*/
		wearEnabled( offer: MallOffer, state: MallState ) {
			const entry = state.tabs.find( row => row.shop === offer.shop && row.tab === offer.tab );
			return entry?.category === "MALL_AVATAR" && (entry.tab === 1 || entry.tab === 2);
		},
		/*
		================
		wear
		================
		*/
		wear( offer: MallOffer, state: MallState ) {
			if ( !this.canWear( offer, state ) ) return;
			popup = -1;
			worn.set( offer.tab, offer );
		},
		/*
		================
		takeOff
		================
		*/
		takeOff() {
			worn.clear();
		},
		/*
		================
		previewRequest
		================
		*/
		previewRequest(): readonly number[] | null {
			return visible ? [ ...worn.values() ].flatMap( offer => [ ...offer.itemIds ] ) : null;
		},
		/*
		================
		previewGid

		The mannequin's actor once it is built; the skin change window shows
		the same mannequin.
		================
		*/
		previewGid(): number | undefined {
			return preview.gid;
		},
		/*
		================
		previewState
		================
		*/
		previewState( value: MallPreviewState ) {
			if ( value === preview ) return false;
			preview = value;
			for ( const [slot, offer] of worn ) {
				if ( offer.itemIds.some( id => !value.wearable.includes( id ) ) ) worn.delete( slot );
			}
			return true;
		},
		/*
		================
		askReserve
		================
		*/
		askReserve( offer: MallOffer ) {
			if ( awaiting || queue.length ) return;
			popup = -1;
			selected = null;
			points = 0;
			pointDialog = false;
			question = { kind: basket.has( offer.packageId ) ? "remove" : "reserve", offers: [ offer ] };
		},
		/*
		================
		askBatch
		================
		*/
		askBatch( kind: "worn" | "basket", state: MallState ) {
			if ( state.pending || awaiting || queue.length ) return;
			const offers = kind === "worn" ?
				[ ...worn.values() ] :
				state.offers.filter( offer => basket.has( offer.packageId ) );
			if ( offers.length ) {
				selected = null;
				points = 0;
				pointDialog = false;
				question = { kind, offers };
			}
		},
		/*
		================
		cancelQuestion
		================
		*/
		cancelQuestion() {
			pointDialog = false;
			question = null;
		},
		/*
		================
		confirmQuestion

		6BBAB0 admits the sequence only after affirmative confirmation. Reserve
		and delete are local selections; purchases remain native single requests.
		================
		*/
		confirmQuestion( state: MallState ) {
			if ( !question || !questionReady( state ) ) return;
			if ( question.kind === "reserve" ) {
				for ( const offer of question.offers ) basket.add( offer.packageId );
			} else if ( question.kind === "remove" ) {
				for ( const offer of question.offers ) basket.delete( offer.packageId );
			} else {
				let remaining = points;
				queue = question.offers.map( offer => {
					const contribution = offer.allowsPoints ? Math.min( remaining, offer.silk ) : 0;
					remaining -= contribution;
					return { ...offer, quantity: 1, points: contribution };
				} );
			}
			question = null;
		},
		/*
		================
		takeNextPurchase

		One receipt advances one element. A rejection, close or session reset
		cancels unsent elements. A lost receipt is never retried automatically.
		================
		*/
		takeNextPurchase( state: MallState ): MallPurchase | null {
			if ( !visible || awaiting || state.pending || !queue.length ) return null;
			const next = queue[0]!;
			const current = currentOffer( next, state );
			if (
				!current || current.silk !== next.silk || current.giftSilk !== next.giftSilk ||
				current.silk - next.points > state.silk + state.earnedSilk || current.giftSilk > state.giftSilk ||
				next.points > state.points
			) {
				queue = [];
				return null;
			}
			awaiting = { revision: state.revision, offer: current };
			return { ...current, quantity: 1, points: next.points };
		},
		questionReady,
		reset
	};
}
