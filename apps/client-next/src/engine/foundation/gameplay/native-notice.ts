import {nativeNoticeRoute} from './native-notice-data';
import type {SystemNotice} from './system-notices';
export interface NativeNoticeContext {readonly country?:number;readonly itemMallOpen?:boolean;readonly war?:boolean;readonly pkProhibited?:boolean;}
export type NoticeResolution={readonly kind:'notice';readonly notice:SystemNotice}|{readonly kind:'silent'}|{readonly kind:'context';readonly category:number;readonly code:number};
export function resolveNativeNotice(category:number,code:number,context:NativeNoticeContext={}):NoticeResolution{
 const route=nativeNoticeRoute(category,code);
 const notice=(key:string,guide:0|1|4|5|null,banner:boolean):NoticeResolution=>key?{kind:'notice',notice:{key,value:0,nativeType:guide??0,...(banner?{banner:true as const}:{}),...(guide===null?{bannerOnly:true as const}:{})}}:{kind:'silent'};
 if(route.kind==='notice')return notice(route.key,route.guideType,route.banner||(category===1&&code===7&&context.itemMallOpen===true));
 if(route.kind==='silent')return route;
 if(category===1&&code===50){if(context.country===undefined)return route;return notice(context.country===0?'UIIT_MSG_STRGERR_CANT_MIX_EXCLUSIVE_ARMOR_TYPE':context.country===1?'UIIT_MSG_STRGERR_EU_CANT_MIX_EXCLUSIVE_ARMOR_TYPE':'',5,true);}
 if(category===4&&code===14){if(context.country===undefined)return route;return notice(context.country===0?'UIIT_SKILL_USE_FAIL_RUNOUT_AMMO':context.country===1?'UIIT_SKILL_USE_FAIL_RUNOUT_AMMO_VOLT':'',null,true);}
 if(category===4&&(code===22||code===23)){if(context.pkProhibited===undefined)return route;return notice(context.pkProhibited?'UIIT_MSG_SKILL_USE_FAIL_PK_PROHIBITED_IN_THIS_SERVER':code===22?'UIIT_MSG_SKILL_USE_FAIL_YOUR_LEVEL_TOO_LOW_TO_PK':'UIIT_MSG_SKILL_USE_FAIL_TARGET_LEVEL_TOO_LOW_TO_PK',0,false);}
 if(category===4&&code===32){if(context.war===undefined)return route;return notice(context.war?'UIIT_MSG_SKILL_USE_FAIL_CANT_ATTACK_FORTRESS':'UIIT_MSG_SKILL_USE_FAIL_CANT_ATTACK_TEMPORARILY',0,true);}
 // 68A489 formats 100; 68A4B6 looks up the formatted banner key,
 // while 68A4CB writes the formatted text directly to the guide.
 if(category===12&&code===8)return {kind:'notice',notice:{key:'UIIT_MSG_COSERR_TOO_FAR_FROM_TRADECART',value:100,arguments:['100'],nativeType:0,banner:true,bannerUsesFormattedKey:true}};
 // 68AA9C formats the literal 500,000,000 with grouping width 3, then
 // substitutes it into the localized guild-war compensation template.
 if(category===16&&code===94)return {kind:'notice',notice:{key:'UIIT_MSG_GUILDWARERR_LIMIT_COMPENSATION',value:0,arguments:['500,000,000'],nativeType:0,banner:true}};
 // 68A988..68AA1F: simple modal, not a guide/banner. Localization stays
 // in presentation; both strings are joined by the native raw LF.
 if(category===16&&code===67)return {kind:'notice',notice:{key:'',value:0,bannerOnly:true,dialog:{title:'UIIT_STT_EVENTGUIDE',lines:['UIIT_MSG_GUILDWAR_SUGGESTIONS_01','UIIT_MSG_GUILDWAR_SUGGESTIONS_02']}}};
 return route;
}
// Constant-only caller. Contextual branches must be handled by their owner;
// never silently pretend an unknown dynamic route is a native no-op.
export function constantNativeNotice(category:number,code:number):SystemNotice|null{
 const result=resolveNativeNotice(category,code);
 if(result.kind==='context')throw Error(`Native notice requires context: ${category}:${code}`);
 return result.kind==='notice'?result.notice:null;
}
