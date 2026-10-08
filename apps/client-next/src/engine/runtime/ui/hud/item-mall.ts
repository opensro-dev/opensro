/*
===========================================================================

item-mall.ts - native mall navigation and confirmation lifetime

Owns browsing choices only. The gameplay publication owns catalogue, balances
and purchase completion. Closing a dialog never retries a purchase.

===========================================================================
*/

import type { MallOffer, MallState, MallPreviewState } from "@/engine/contracts/item-mall";
import { mallCategories, MALL_ROWS_PER_PAGE } from "@/engine/foundation/ui/item-mall-layout";
import type { MallPurchase } from "@/engine/foundation/gameplay/item-mall-wire";

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
		return points <= state.points && points <= pointLimit() && silk - points <= state.silk &&
			gift <= state.giftSilk;
	}

	/*
	================
	reset
	================
	*/
	function reset() {
		visible = false;
		category = -1;
		tab = 0;
		page = 0;
		pageSize = MALL_ROWS_PER_PAGE;
		selected = null;
		pointDialog = false;
		quantity = 1;
		points = 0;
		pointDraft = 0;
		bagPage = 0;
		compactView = "shop";
		compactBagPage = 0;
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
			pointDialog = false;
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
			if ( !Number.isInteger( index ) || index < -1 || index >= mallCategories().length ) return;
			if ( !Number.isInteger( nextTab ) || nextTab < 0 ) return;
			category = index;
			compactView = "shop";
			tab = nextTab;
			page = 0;
			selected = null;
			pointDialog = false;
		},
		/*
		================
		paginate
		================
		*/
		paginate( next: number, count: number ) {
			if ( !Number.isInteger( next ) || !Number.isInteger( count ) || count < 0 ) return;
			page = Math.max( 0, Math.min( Math.ceil( count / pageSize ) - 1, next ) );
		},
		/*
		================
		setPageSize

		Presentation only: keep the first visible offer on rotation. Action indices
		continue to address read().offers, including purchases and basket changes.
		================
		*/
		setPageSize( count: number ) {
			if ( !Number.isInteger( count ) || count < 1 || count > MALL_ROWS_PER_PAGE ) return;
			page = Math.floor( page * pageSize / count );
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
			if ( selected.silk * quantity - points > state.silk || selected.giftSilk * quantity > state.giftSilk ) {
				return null;
			}
			return { ...selected, quantity, points };
		},
		/*
		================
		read
		================
		*/
		read( state?: MallState ) {
			const key = mallCategories()[category]?.key;
			const tabs = state?.tabs.filter( entry => entry.category === key ) ?? [];
			const current = tabs[tab];
			const offers = state?.offers.filter( offer =>
				key === "basket" ?
					basket.has( offer.packageId ) :
					current && offer.shop === current.shop && offer.tab === current.tab
			) ?? [];
			const currentPage = Math.min( page, Math.max( 0, Math.ceil( offers.length / pageSize ) - 1 ) );
			return {
				question,
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
				category,
				tab,
				page: currentPage,
				pageSize,
				selected,
				quantity,
				points,
				tabs,
				count: offers.length,
				offers: offers.slice( currentPage * pageSize, (currentPage + 1) * pageSize )
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
				current.silk - next.points > state.silk || current.giftSilk > state.giftSilk ||
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
