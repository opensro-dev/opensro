import type {CatalogMessage} from './message';
import type {WorldCamera} from "./scene";
export interface CreationSelection {readonly race:0|1;readonly gender:0|1;readonly figure:number;readonly height:number;readonly volume:number;readonly weapon:number;readonly protector:number;readonly name:string;}
export interface CreationSnapshot {readonly selection:CreationSelection;readonly protectorFloor:0|1;readonly explain:'figure'|'height'|'volume'|'weapon'|'protector';readonly phase:'editing'|'checking'|'confirming'|'dismissing'|'submitting'|'accepted';readonly alpha:number;readonly status?:CatalogMessage;readonly ready:boolean;readonly yaw:number;readonly zoom:boolean;readonly camera:WorldCamera;}
export interface FrontendDialog {readonly kind:'delete-character'|'restore-character';readonly character:string;readonly id:number;readonly phase:'opening'|'open'|'pending'|'closing';readonly alpha:number;readonly operationId?:number;}
export interface FrontendCameraKey {
 readonly timeSeconds:number; readonly sectorX:number; readonly sectorY:number;
 readonly position:{readonly x:number;readonly y:number;readonly z:number};
 readonly rotation:{readonly x:number;readonly y:number;readonly z:number}; readonly mode:number;
}
export interface FrontendCameraTrack {readonly keys:readonly FrontendCameraKey[];readonly target:number;readonly mode:"intro"|"transition";}
export type FrontendPhase="loading-title"|"intro"|"login-reveal"|"login"|"login-accepted"|"loading-dock"|"dock-arrival"|"dock"|"create-arrival"|"create"|"create-return"|"race-zoom"|"loading-create"|"customize"|"create-exit"|"loading-race"|"title-exit"|"title-logout"|"departing"|"loading-world"|"world"|"failed";
export interface FrontendSnapshot {readonly entryPending?:boolean;readonly loadingStatus?:string;readonly loadingProgress?:number;readonly phase:FrontendPhase;readonly generation:number;readonly elapsed:number;readonly alpha:number;readonly logoAlpha:number;readonly error:string|null;readonly selectedCharacter?:string;readonly cameraMoving?:boolean;readonly camera?:WorldCamera;readonly hoveredCharacter?:number|null;readonly dialog?:FrontendDialog|null;readonly status?:CatalogMessage;readonly hoveredRace?:0|1|null;readonly raceCenters?:readonly (readonly [number,number,number]|null)[];readonly creation?:CreationSnapshot|null;readonly race?:0|1;}
export interface FrontendCameraFrame {readonly camera:WorldCamera;readonly time:number;readonly complete:boolean;}
export interface StageManifest {camera:FrontendCameraKey[];cameraControllerTargetTimeSeconds:number;createCamera?:FrontendCameraKey[];createCameraControllerTargetTimeSeconds?:number;regionBundlePublicPath:string;}
