export interface BerserkGauge {readonly authoritative:number;readonly displayed:number;readonly pending:number;}
export interface ItemEffectFeedback {readonly kind:'item-effect';readonly source:import('./world').EntityState;readonly item:number;readonly typeFlags:number;}
export type VisualFeedback=ItemEffectFeedback|OrbFeedback|{readonly kind:'level-up';readonly gid:number}|{readonly kind:'pet-appear';readonly gid:number};
export type OrbFeedback={readonly kind:'orb-gauge';readonly value:number}|{readonly kind:'orb-feedback';readonly source:number;readonly color:0|1|2|3;readonly count:number;readonly target:number}|{readonly kind:'orb-clear'};
