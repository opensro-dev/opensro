/*
===========================================================================

title-contract.ts - shapes shared by the title screens

The native layout nodes the title resources load, and the interaction state
ui.ts hands the title renderer each frame.

===========================================================================
*/
/*
================
TitleNode
================
*/
export interface TitleNode {
	id: number;
	name: string;
	rect: { x: number; y: number; width: number; height: number; };
	clientRect?: { x: number; y: number; width: number; height: number; };
	ddj?: { publicPath: string; };
	text?: string;
	fontIndex?: number;
	fontColor?: { r: number; g: number; b: number; a: number; };
	hAlign?: number;
	vAlign?: number;
}
/*
================
TitleLayout
================
*/
export interface TitleLayout {
	controlsByName: Record<string, TitleNode>;
	sections: { name: string; nodes: TitleNode[]; }[];
}
/*
================
TitleInteraction
================
*/
export interface TitleInteraction {
	hover: string | null;
	pressed: string | null;
	focus: string | null;
	draft: string;
	offset: number;
	now: number;
	message: string;
	selection?: readonly number[];
	// A login request is in flight: the account and password edits are locked.
	// Other pending work (a server-list refresh) leaves them editable.
	credentialsLocked?: boolean;
	registrationEnabled?: boolean;
	registering?: boolean;
	confirmPassword?: string;
}
