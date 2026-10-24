import {feedbackLevels} from '@/engine/foundation/gameplay/feedback-levels';
import type {OrbFeedback} from '@/engine/contracts/orb';
// 776DB0: float ABI, truncating conversion, then native 1..10 clamp.
export function feedbackCount(value:number){const f=Math.fround(value);let n=3-Math.trunc((1-f)*5);if(f>=1||Number.isNaN(f)){n=10;for(let bound=64;n>3&&(bound>f||Number.isNaN(f));bound*=0.5)n--;}return Math.max(1,Math.min(10,n));}
export function createFeedback(){
 const levels=feedbackLevels();let level:number|undefined,experience=0n,gauge=0,skillExperience=0;
 function advance(current:number,xp:bigint,delta:number){let next=current,total=xp+BigInt(delta);while(total<0n){next--;const row=levels.get(next);if(!row)throw Error('Missing previous level authority');total+=BigInt(row[0]);}while(true){const row=levels.get(next);if(!row)throw Error('Missing level authority');if(total<BigInt(row[0]))break;total-=BigInt(row[0]);next++;}return {level:next,experience:total};}
 return {
  bootstrap(value:unknown){const c=(value as {character?:{level?:number;experience?:number|string;skillExp?:number;berserkPoints?:number}}).character;skillExperience=c?.skillExp??0;if(!Number.isSafeInteger(skillExperience)||skillExperience<0||skillExperience>0xffffffff)throw Error('Invalid skill experience');level=c?.level;experience=BigInt(c?.experience??0);gauge=c?.berserkPoints??0;if(!Number.isInteger(gauge)||gauge<0||gauge>5)throw Error("Invalid Berserk bootstrap");if(level!==undefined&&(!levels.has(level)||experience<0n||typeof c?.experience==='number'&&!Number.isSafeInteger(c.experience)))throw Error('Invalid experience authority');},
  gauge(){return gauge;},
  receive(op:number,p:Uint8Array,local:number,cos?:{gid:number;level?:number;experience?:readonly [number,number]}){
   const v=new DataView(p.buffer,p.byteOffset,p.byteLength),events:OrbFeedback[]=[];
   if(op===0x30b3&&p[0]===4){if(p.length!==6||p[1]!>5)throw Error('Invalid berserk gauge update');const next=p[1]!,source=v.getUint32(2,true);events.push({kind:'orb-gauge',value:next});if(next>gauge&&source)events.push({kind:'orb-feedback',source,target:local,color:2,count:next-gauge});else events.push({kind:'orb-clear'});gauge=next;return {events};}
   if(op===0x30d2){
    if(p.length<13)throw Error('Truncated experience update');if(level===undefined)throw Error('Missing experience bootstrap');const source=v.getUint32(0,true),delta=v.getInt32(4,true),skill=v.getInt32(8,true),flags=p[12]!;
    const next=advance(level,experience,delta),end=13+((flags&15)===1?4:0)+((flags&240)===16?8:0)+(next.level>level?2:0);
    if(source&&delta>0)events.push({kind:'orb-feedback',source,target:local,color:0,count:feedbackCount(delta/levels.get(next.level)![1])});if(source&&skill>0)events.push({kind:'orb-feedback',source,target:local,color:1,count:feedbackCount(skill/100)});
    // 779620 reads the stat-point tail only when its own walk crosses, and
    // never checks the frame length: an update whose crossing the frame
    // carries no tail for (a replay onto kept state after a resume) still
    // applies, keeping the previous stat points.
    const statPoints=next.level>level&&p.length>=end?v.getUint16(end-2,true):undefined;level=next.level;experience=next.experience;skillExperience=((skillExperience+skill)>>>0)%400;const messages=[...(delta!==0?[{key:delta>0?'UIIT_MSG_STATE_GAIN_EXP_NEW':'UIIT_MSG_STATE_LOSE_EXP_NEW',value:Math.abs(delta)}]:[]),...(skill>0?[{key:'UIIT_MSG_STATE_GET_SKILL_EXP',value:skill}]:[])];return {events,level,experience:experience.toString(),skillExperience,statPoints,messages};
   }
   if(op===0x3508&&p[4]===3){
    if(p.length!==13)throw Error('Invalid COS experience update');if(!cos||cos.level===undefined||!cos.experience)return {events};
    const delta=v.getInt32(5,true),source=v.getUint32(9,true),xp=BigInt(cos.experience[0])+(BigInt(cos.experience[1])<<32n);
    // 77A8AB..77A8D4: pet losses clamp EXP at zero; only the PC lane demotes.
    const total=xp+BigInt(delta),next=delta>0?advance(cos.level,xp,delta):{level:cos.level,experience:total<0n?0n:total};
    if(source&&delta>0)events.push({kind:'orb-feedback',source,target:cos.gid,color:3,count:feedbackCount(delta/levels.get(next.level)![1])});
    const messages=delta!==0?[{key:delta>0?'UIIT_MSG_COSPET_GAIN_EXP':'UIIT_MSG_COSPET_LOST_EXP',value:Math.abs(delta)}]:[];
    return {events,messages,cos:{...cos,level:next.level,experience:[Number(next.experience&0xffffffffn),Number(next.experience>>32n)] as const}};
   }
   return null;
  },
  reset(){level=undefined;experience=0n;gauge=0;skillExperience=0;}
 };
}
