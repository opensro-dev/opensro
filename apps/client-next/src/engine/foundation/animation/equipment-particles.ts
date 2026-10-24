import type {ModelParticle} from './model-particles';
export function validateEquipmentParticles(value:Record<string,readonly ModelParticle[]>|undefined){
 if(value===undefined)return;
 for(const [id,rows]of Object.entries(value)){
  if(!/^\d+$/.test(id)||!Array.isArray(rows)||rows.length>128)throw Error('Invalid equipment particle catalog');
  for(const row of rows){
   if(!row||typeof row.effectPath!=='string')throw Error('Invalid equipment particle row');
   const path:string=row.effectPath;
   if(!path.endsWith('.efp')||path.includes('..')||path.startsWith('/')||path.includes(':')||typeof row.bone!=='string'||!row.bone||row.root!==false||typeof row.scale!=='number'||!Number.isFinite(row.scale)||row.scale<=0||!Array.isArray(row.offset)||row.offset.length!==3||![...row.offset].every(Number.isFinite)||(row.threshold!==undefined&&(!Number.isInteger(row.threshold)||row.threshold<1||row.threshold>255)))throw Error('Invalid equipment particle row');
  }
 }
}
