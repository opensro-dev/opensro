import {dockSlot} from "./dock-slots";
import type {CharacterRecord} from "@/engine/contracts/session";
import type {FrontendCameraKey} from "@/engine/contracts/frontend";
// CPSCharacterSelect_SetSelectionControlsVisible: slot target + (2,0,2), native yaw (before GLB conversion),
// body-height-dependent focus and eight-unit camera distance.
export function selectedDockCamera(row:CharacterRecord,index:number,count:number):FrontendCameraKey {
 const slot=dockSlot(index,count),height=row.visualLoadout.heightScale;
 return {timeSeconds:2,sectorX:81,sectorY:105,
  position:{x:slot.x+2,y:height*(row.deletePending?9:16)-(row.deletePending?29:28.779172897338867),z:slot.z+2},
  rotation:{x:0,y:.39250001311302185-(Math.PI-slot.yaw),z:0},mode:8};
}
