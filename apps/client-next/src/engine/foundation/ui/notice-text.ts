import type { SystemNotice } from "@/engine/foundation/gameplay/system-notices";
// 9C422E: formatting is a pure projection shared by two independent lifetimes.
export function noticeText(
	copy: ( key: string ) => string,
	notice: SystemNotice,
	channel: "guide" | "banner" = "guide"
) {
	if ( !notice.key ) return notice.text ?? "";
	const template = copy( notice.key );
	// Secure native formatting rejects malformed conversions. Typed callers must
	// not reinterpret date integers as string pointers when a catalog is broken.
	// This projection returns no text; it does not emulate CRT process termination.
	if ( notice.formatKinds ) {
		let at = 0;
		for ( const match of template.matchAll( /%(?:%|(I64|ll|l)?([sdu]))|%/g ) ) {
			if ( match[0] === "%%" ) continue;
			if ( !match[2] || match[2] !== notice.formatKinds[at++] || match[1] ) return "";
		}
	}
	let index = 0;
	const formatted = template.replace(
		/%%|%(I64|ll|l)?([sdu])/g,
		( token, length: string | undefined, kind: string | undefined ) => {
			if ( token === "%%" ) return "%";
			const key = notice.localizedArguments?.[index], argument = key ? copy( key ) : notice.arguments?.[index];
			index++;
			const value = argument ?? (kind === "s" ? (notice.text ?? "") : String( notice.value ));
			if ( kind === "s" || !/^[-+]?\d+$/.test( value ) ) return value;
			// Windows native long is 32-bit; I64/ll preserve the 64-bit payload.
			const bits = length === "I64" || length === "ll" ? 64 : 32, integer = BigInt( value );
			return String( kind === "d" ? BigInt.asIntN( bits, integer ) : BigInt.asUintN( bits, integer ) );
		}
	);
	if ( notice.formatCapacity !== undefined && formatted.length >= notice.formatCapacity ) return "";
	// Native 68A4B6 re-localizes the formatted banner key, independently
	// of the direct guide string. A missing key must remain empty.
	const line = channel === "banner" && notice.bannerUsesFormattedKey ? copy( formatted ) : formatted;
	// Port-only, not native (#4136): a notice may carry a literal tail
	// (a gate link's required level); it rides after the localized line.
	return notice.suffix ? line + notice.suffix : line;
}
