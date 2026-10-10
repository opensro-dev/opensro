import type {AlchemyMode} from '@/engine/contracts/item-process';
import type {InventoryItem} from '@/engine/contracts/gameplay';
export function alchemySlotCapacity(mode:AlchemyMode){return mode==='compound'?9:mode==='advanced'?5:mode==='reinforce'?3:2;}
// 62B6D0 -> 6252B0/550600/550730/550660: the reagent selects the
// operation on the reinforcement page; 7023C0 maps modes 1/2 to wire 2/3.
export function alchemySelection(mode:AlchemyMode,slots:readonly number[],item:InventoryItem):{mode:AlchemyMode;slots:number[]}|null {
 const index=slots.indexOf(item.slot);if(index>=0)return {mode,slots:index===0?[]:slots.filter(s=>s!==item.slot)};
 if(item.slot<13)return null;
 const flags=item.typeFlags&0xfffe,equipment=(flags&0x7e)===0x2c,process=['compound','advanced','dissolve'].includes(mode);
 if(!slots.length){
  if(process){if(equipment)mode='dissolve';else if(flags===0x1dec)mode='advanced';else if(flags===0x25ec||flags===0x35ec)mode='compound';else return null;}
  else if(!equipment)return null;
 }else if(!process){
  // 62B6D0: magic stones (3/3/11/1 0x0dec, 3/3/11/7 0x3dec) take the magic-option
  // branch, attribute stones (3/3/11/2 0x15ec) the attribute one.
  if(flags===0x0dec||flags===0x3dec)mode='magic';else if(flags===0x15ec)mode='attribute';else if(flags===0x0d6c)mode='reinforce';else if(flags!==0x156c||mode!=='reinforce')return null;
  if(slots.length>1&&flags!==0x156c)return {mode,slots:[slots[0]!,item.slot]};
 }else if(mode==='compound'&&flags!==0x25ec&&flags!==0x35ec||mode==='advanced'&&flags!==0x2dec||mode==='dissolve'&&flags!==0x35ec)return null;
 const capacity=alchemySlotCapacity(mode);
 return slots.length>=capacity?null:{mode,slots:[...slots,item.slot]};
}
