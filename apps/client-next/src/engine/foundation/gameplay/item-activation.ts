import type {GameplayCommand,InventoryItem} from '@/engine/contracts/gameplay';
// CIFEquipment TID/socket table, 594980/594d7c. The server repeats equipment
// requirements against authority; this only chooses the requested destination.
export function equipmentSocket(flags:number):number|null{
 if((flags&0x1e)!==0x0c)return null;
 const band=flags&0x60,group=flags>>>7&15,sub=flags>>>11&31;
 if(band===0x60&&group===4)return 7;
 if(band!==0x20)return null;
 if(group===6)return 6;if(group===4&&(sub===1||sub===2))return 7;if(group===7)return 8;
 if([1,2,3,9,10,11].includes(group))return [0,2,1,4,3,5][sub-1]??null;
 if(group===5||group===12)return [9,10,11][sub-1]??null;
 return null;
}
export function itemActivation(slot:number,inventory:readonly InventoryItem[],capacity:number|undefined,pending:boolean):GameplayCommand|null{
 const item=inventory.find(item=>item.slot===slot);if(!item||pending)return null;
 const socket=equipmentSocket(item.typeFlags);
 if(slot<13){
  if(capacity===undefined)return null;
  for(let destination=13;destination<capacity;destination++)if(!inventory.some(item=>item.slot===destination))return {kind:'inventory-move',source:slot,destination,quantity:0,activation:true};
  return null;
 }
 if(socket!==null){const destination=socket===11&&inventory.some(item=>item.slot===11)&&!inventory.some(item=>item.slot===12)?12:socket;return {kind:'inventory-move',source:slot,destination,quantity:0,activation:true};}
 return {kind:'item-use',slot};
}
