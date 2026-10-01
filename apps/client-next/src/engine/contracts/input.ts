/** Native wheel units (120 per detent), positive away from the character. */
export type CameraWheelDelta=number&{readonly cameraWheelDelta:unique symbol};
export type RawInput = {
    kind: "pointer";
    x: number;
    y: number;
    buttons: number;
    timeMs: number;
} | {
    kind: "key";
    code: string;
    down: boolean;
    timeMs: number;
} | {
    kind: "wheel";
    delta: CameraWheelDelta;
    timeMs: number;
} | {
    kind: "release";
    timeMs: number;
};
export type InputCommand = RawInput & {
    sequence: number;
};
export interface InputBatch {
    first: number;
    last: number;
    commands: readonly InputCommand[];
}
export interface InputOwner {
    dropNameBinding(value:number):void;
    dropNamesHeld():boolean;
    blindBinding(value:number):void;
    blindHeld():boolean;
    mouseMode(value:0|1):void;
    sight(value:import("@/engine/foundation/rendering/camera-options").SightMode):void;
    camera(playerYaw?:number): CameraInput;
    accept(event: RawInput): void;
    drain(): InputBatch | null;
    error(): string | null;
}
export interface SimulationInput {
    receive(batch: InputBatch): void;
    commit(): number;
    lastAccepted(): number;
}
export interface CameraInput {readonly yaw:number;readonly pitch:number;readonly distance:number;}
