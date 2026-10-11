export interface NavOutlineGrid {readonly x:number;readonly z:number;readonly nx:number;readonly nz:number;readonly offsets:Uint32Array;readonly edgeIds:Uint16Array;}
export interface NavMesh {readonly vertexDirections?:Uint8Array;readonly outlineGrid?:NavOutlineGrid;readonly cellWords?:Uint16Array;readonly cellEvents?:Uint8Array;readonly edgeEvents?:Uint8Array;readonly eventNames?:readonly string[];readonly vertices:Float32Array;readonly cells:Uint16Array;readonly edges:Uint32Array;readonly bounds:readonly number[];readonly passThrough:boolean}
export interface NavPlacement {readonly terrainCells?:readonly (readonly [number,number,number,number])[];readonly x:number;readonly y:number;readonly z:number;readonly yaw:number;readonly mesh:NavMesh;readonly links?:readonly NavLink[];readonly block?:number;readonly floor?:number;readonly obstacles?:readonly NavObstacle[]}
export interface NavigationProduct {readonly regionId:number;readonly navmesh?:unknown;readonly objects:readonly NavPlacement[];readonly complete:boolean}

export interface ObjectNavWire {sourcePath:string;byteLength:number;headerOffsets:number[];nativePayloads?:{kind:string;byteOffset:number;byteLength:number;rawBase64:string}[]}
export interface NavBsr {objectId:number;sourcePath:string;primaryMeshPath?:string|null;renderMeshSection?:{paths:string[]};meshPaths?:string[]}
export interface NavResources {bsr:NavBsr[];meshes?:ObjectNavWire[];meshFiles?:{sourcePath:string;publicPath:string}[]}
export interface NavRegion {dx:number;dz:number;blockedTiles:string;tileCellIds:string;heightMap:string;planeType?:string;planeHeight?:string;cells:{count:number;minX?:string;minZ?:string;maxX?:string;maxZ?:string;objectIndexOffsets?:string;objectIndices?:string};objects?:{assetId:number;x:number;y:number;z:number;yaw:number;linkEdgeCount?:number;linkEdges?:string}[]}
export interface NavBundle {source:{sectorX:number;sectorY:number};navmesh:{regions:NavRegion[]};objects:{resources?:NavResources;resourceIndexPublicPath:string}}
export interface DungeonManifest {format:string;version:number;entries:{sectorId:number;normalizedName:string}[];resources:{normalizedName:string;byteLength:number;rawBase64:string}[];navResources:{bsr:NavBsr[];meshes:ObjectNavWire[]}}

export interface NavLink {readonly edge:number;readonly target:number;readonly targetEdge:number}

export interface NavObstacle {readonly x:number;readonly y:number;readonly z:number;readonly radiusSquared:number}

// Coordinates are relative to originRegion, never absolute world coordinates.
export interface GroundPickQuery {readonly originRegion:number;readonly ray:import('@/engine/foundation/rendering/picking').PickRay;readonly terrainDepth:number|null;}

// A motion owner's cursor is valid only for one navigation admission revision.
export interface SurfaceCursor {revision?:number;owner?:import('@/engine/foundation/navigation/dungeon-ownership').NavOwner;}
export type SurfaceResolver=(pose:import('./gameplay').Pose,reference:import('./gameplay').Pose,cursor?:SurfaceCursor)=>import('./gameplay').Pose;
