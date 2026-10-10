import type {AlchemyMode,AlchemyOutcome,AlchemyState} from '@/engine/contracts/item-process';
import type {InventoryItem} from '@/engine/contracts/gameplay';
import type {UiSoundHandle} from '@/engine/foundation/ui/sound-catalog';
// CIFAlchemyReinforce owns presentation state; inventory owns all item mutations.
export function createAlchemy(){
 let state:AlchemyState={visible:false,pending:false,mode:'reinforce',flags:0,error:null,slot:null};
 let outcomes=0;
 return {
  open(){state={...state,visible:true};},
  close(){state={...state,visible:false};},
  start(mode:AlchemyMode,selected:readonly number[],items:ReadonlyMap<number,InventoryItem>,now=0,quantity=1){
   const process=['compound','advanced','dissolve'].includes(mode);
   if(!state.visible||state.pending||!['reinforce','attribute','magic','compound','advanced','dissolve'].includes(mode)||now<(state.lockUntil??0)||selected.length<(['compound','dissolve'].includes(mode)?1:2)||selected.length>(mode==='compound'?9:mode==='dissolve'?2:5)||new Set(selected).size!==selected.length)throw Error('Alchemy operation unavailable');
   for(const slot of selected)if(!Number.isInteger(slot)||slot<13||slot>255||!items.has(slot))throw Error('Alchemy slot unavailable');
   if(!process&&(items.get(selected[0]!)!.typeFlags&0x7e)!==0x2c)throw Error('Alchemy target must be equipment');
   if(process){
    if(mode==='dissolve'&&quantity!==1)throw Error('Dissolve has no quantity field');
    if(mode==='advanced'&&quantity!==1)throw Error('Tablet manufacture requires quantity one');
    const available=mode==='compound'?selected.reduce((sum,slot)=>sum+((items.get(slot)!.typeFlags&0xfffe)===0x35ec?0:items.get(slot)!.quantity),0):items.get(selected[0]!)!.quantity;
    if(!Number.isSafeInteger(quantity)||quantity<1||quantity>0xffffffff||quantity>available)throw Error('Invalid compound quantity');
    let opcode:number,payload:Uint8Array;
    if(mode==='dissolve'){opcode=0x7549;payload=Uint8Array.of(selected.length,selected[0]!,selected[1]??12);}
    else {opcode=0x716f;const slots=mode==='advanced'?Array.from({length:5},(_,i)=>selected[i]??12):selected;payload=new Uint8Array(7+slots.length);payload[0]=2;payload[1]=mode==='compound'?1:2;new DataView(payload.buffer).setUint32(2,quantity,true);payload[6]=selected.length;payload.set(slots,7);}
    state={visible:true,pending:true,mode,flags:0,error:null,slot:selected[0]!,remaining:quantity,total:quantity,lockUntil:now+2000,locked:true,cancelled:false};return {opcode,payload};
   }
   const opcode=mode==='reinforce'?0x7373:0x7651,payload=Uint8Array.from([...(mode==='reinforce'?[]:[mode==='attribute'?2:3]),selected.length,...selected]);
   state={visible:true,pending:true,mode,flags:0,error:null,slot:selected[0]!};return {opcode,payload};
  },
  result(slot:number,flags:number,error:number|null=null,detail?:Omit<AlchemyOutcome,'sequence'|'flags'>):readonly UiSoundHandle[]{
   state={...state,pending:false,slot,flags,error,...(detail&&flags?{outcome:{...detail,flags,sequence:++outcomes}}:{})};
   if(!state.visible||!flags)return [];
   // 62B0B0: use always precedes the result cue; destroyed/cancelled reuse failure.
   return ['SND_ELIXIR_USE',flags&0x10?'SND_ELIXIR_SUCCESS':'SND_ELIXIR_FAILURE'];
  },
  cancel(){if(!state.pending||!['compound','advanced','dissolve'].includes(state.mode))return null;state={...state,pending:false,cancelled:true,remaining:0};return {opcode:0x716f,payload:Uint8Array.of(1)};},
  compound(success:boolean,completed?:number,error:number|null=null):readonly UiSoundHandle[]{
   if(state.cancelled)return [];
   const count=Math.max(0,(state.remaining??0)-(completed??0));state={...state,pending:success&&state.mode==='compound'&&count>0,remaining:count,error};return state.visible&&success?['SND_ELIXIR_USE']:[];
  },
  step(now:number){if(state.locked&&now>=(state.lockUntil??0)){state={...state,locked:false};return true;}return false;},
  state:()=>state,
  reset(){state={visible:false,pending:false,mode:'reinforce',flags:0,error:null,slot:null};}
 };
}
