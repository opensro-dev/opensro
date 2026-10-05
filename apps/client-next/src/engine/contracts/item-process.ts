export type AlchemyMode='reinforce'|'attribute'|'magic'|'compound'|'advanced'|'dissolve';
export type ItemProcessCommand=
 | {readonly kind:'alchemy-open'} | {readonly kind:'alchemy-close'} | {readonly kind:'alchemy-cancel'}
 | {readonly kind:'alchemy-start';readonly mode:AlchemyMode;readonly slots:readonly number[];readonly quantity?:number}
 | {readonly kind:'gacha-open';readonly gid:number}
 | {readonly kind:'gacha-close'}
 | {readonly kind:'gacha-roll';readonly entry:number;readonly slot:number};
export interface AlchemyState {readonly locked?:boolean;readonly remaining?:number;readonly total?:number;readonly lockUntil?:number;readonly cancelled?:boolean;readonly visible:boolean;readonly pending:boolean;readonly mode:AlchemyMode;readonly flags:number;readonly error:number|null;readonly slot:number|null;}
export interface GachaState {readonly visible:boolean;readonly phase:'closed'|'opening'|'idle'|'rolling'|'waiting'|'result';readonly npc:number;readonly slot:number|null;readonly entry:number;readonly started:number;readonly result:'win'|'lose'|null;readonly reward?:{readonly refObjId:number;readonly quantity:number};readonly error:number|null;}
// The open player exchange (foundation/gameplay/exchange.ts): each side's
// offers by exchange slot, the bag slot only on the own side.
export interface ExchangeOffer {readonly slot:number;readonly bagSlot?:number;readonly item:import('./gameplay').InventoryItem;}
export interface ExchangeState {readonly open:boolean;readonly partner:number;readonly own:readonly ExchangeOffer[];readonly theirs:readonly ExchangeOffer[];readonly ownGold:number;readonly theirGold:number;readonly ownLocked:boolean;readonly theirLocked:boolean;readonly approved:boolean;readonly requesting:boolean;}
