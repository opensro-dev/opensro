import {createModifierDelta} from './modifier-delta';
// CRTModTexAni::Update, AED870: six float32 stores, delta milliseconds / 1000.
// The matrix accumulates in place for the life of one instance; the world
// renderer starts a fresh one when a scene build respawns the group.
export function createTextureMotion(velocity:readonly number[]){
 if(velocity.length!==6||!velocity.every(Number.isFinite))throw Error('Invalid texture velocity');
 const rates=velocity.slice(),slots=[0,4,1,5,2,6] as const;
 const matrix=new Float32Array([1,0,0,0,0,1,0,0]);const deltaFor=createModifierDelta();
 const stepDelta=(milliseconds:number)=>{
  const delta=Math.fround(milliseconds/1000);
  if(delta<=0)return false;
  for(let i=0;i<6;i++){const slot=slots[i]!;matrix[slot]=matrix[slot]!+rates[i]!*delta;}
  return true;
 };
 return {matrix,stepDelta,step:(seconds:number)=>stepDelta(deltaFor(seconds))};
}
