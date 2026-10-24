import {decodeWeather,type WeatherOptions} from './weather';
export interface EventEnvironment {
 readonly groups:Readonly<Record<number,number>>;
 readonly server:WeatherOptions;
 readonly effective:WeatherOptions;
}
// 77B220 entry, 761430 (3347), 7775A0 (3BDE). 7E2980/7E29E0
// look up the stack-supplied group key; they do not access the map head.
export function entryEnvironment(value:unknown):{state:EventEnvironment;notices:readonly string[]}{
 const ids=(value as {character?:{enterEventGroupIds?:unknown}})?.character?.enterEventGroupIds??[];
 if(!Array.isArray(ids)||ids.length>255||ids.some(id=>!Number.isInteger(id)||id<0||id>0xffffffff))throw Error('Invalid entry event groups');
 const groups:Record<number,number>={},notices:string[]=[];
 for(const id of ids){groups[id]=1;if(id===1)notices.push('UIIT_MSG_EVENT_START');}
 const server:WeatherOptions={mode:1,amount:0};
 return {state:{groups,server,effective:groups[1]===1?{mode:3,amount:20}:server},notices};
}
export function environmentPacket(state:EventEnvironment,opcode:number,p:Uint8Array):{state:EventEnvironment;notice?:string}|null{
 if(opcode===0x3bde){const server=decodeWeather(p);return {state:{...state,server,effective:((state.groups[1]??0)&1)!==0?{mode:3,amount:20}:server}};}
 if(opcode!==0x3347)return null;
 if(p.length!==5)throw Error('Invalid event state packet');
 const id=new DataView(p.buffer,p.byteOffset,p.byteLength).getUint32(0,true),active=p[4]!;
 const next={...state,groups:{...state.groups,[id]:active}};
 // Only exact 0/1 changes weather immediately. Subsequent 3BDE checks bit 0.
 if(id!==1||(active!==0&&active!==1))return {state:next};
 return {state:{...next,effective:active===1?{mode:3,amount:20}:state.server},notice:active===1?'UIIT_MSG_EVENT_START':'UIIT_MSG_EVENT_END'};
}
// The debug console's weather rows (690C40 ids 99/105-107) write the same
// options a 3BDE writes: the server slot, with the event override still
// winning while its group is active. A later 3BDE overwrites it back.
export function debugWeather(state:EventEnvironment,mode:1|2|3|undefined,amount:number):EventEnvironment{
 const server:WeatherOptions={mode:mode??state.server.mode,amount:Math.min(255,Math.max(0,Math.trunc(amount)))};
 return {...state,server,effective:((state.groups[1]??0)&1)!==0?{mode:3,amount:20}:server};
}
