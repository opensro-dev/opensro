export interface PackEntry {publicPath:string;path:string;packPath:string;offset:number;length:number;mime:string;sha256:string;animationSources?:string[];transport?:{path:string;length:number;sha256:string;encoding:'gzip'}}
export interface PackDescriptor {path:string;bytes:number;sha256:string;assetCount:number;entries:PackEntry[];load?:string}
export type PackRange={start:number;end:number;total:number};
export type PackDownload=(url:string,limit:number,signal:AbortSignal,range?:PackRange)=>Promise<Uint8Array<ArrayBuffer>>;
