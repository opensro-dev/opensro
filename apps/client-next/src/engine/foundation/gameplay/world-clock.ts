import type {WorldClockSeed} from '@/engine/contracts/gameplay';
// Native 777410/7774d0 packets, 77e540 integer clock divisor, 728630 sun curve.
export function decodeWorldClock(payload:Uint8Array,offset:number,now:number):WorldClockSeed{
 if(payload.length!==offset+4||!Number.isFinite(now))throw new Error('Invalid world clock packet');const v=new DataView(payload.buffer,payload.byteOffset,payload.byteLength),day=v.getUint16(offset,true),hour=v.getUint8(offset+2),minute=v.getUint8(offset+3);if(hour>=24||minute>=60)throw new Error('Invalid world calendar');return {day,hour,minute,receivedAtMs:now};
}
// The seed's wall time (day, hour, minute, second) at now, before the sun
// mapping: what the debug console's time family reads and re-seeds.
export function worldClockWallTime(seed:WorldClockSeed|undefined,now:number):{day:number;hour:number;minute:number;second:number}|null{
 if(!seed)return null;if(!Number.isFinite(now))throw new Error('Invalid world clock sample');
 const total=((seed.day*24+seed.hour)*60+seed.minute)*60+Math.floor(Math.max(0,now-seed.receivedAtMs)/20);
 const within=total%86400;
 return {day:Math.floor(total/86400),hour:Math.floor(within/3600),minute:Math.floor(within%3600/60),second:within%60};
}
export function sampleWorldClock(seed:WorldClockSeed|undefined,now:number):{timeOfDay:number;lunarDay:number}|null{
 if(!seed)return null;if(!Number.isFinite(now))throw new Error('Invalid world clock sample');
 const total=((seed.day*24+seed.hour)*60+seed.minute)*60+Math.floor(Math.max(0,now-seed.receivedAtMs)/20);
 const seconds=total%86400,hour=Math.fround(Math.fround(seconds/86400)*24);let sunHour:number;
 if(hour>=4&&hour<=20)sunHour=Math.fround(Math.fround((hour-4)*12*.0625)+6);
 else {const h=hour<4?Math.fround(hour+24):hour;sunHour=Math.fround(Math.fround((h-20)*12*.125)+18);if(sunHour>=24)sunHour=Math.fround(sunHour-24);}
 return {timeOfDay:Math.fround(sunHour/24),lunarDay:Math.floor(total/86400)&65535};
}
