// Owned by the asset pipeline: v1.150 data-format tables recovered from the
// native client. Keep behaviour identical to the published assets.
/** Native address of a global std::string animation-set name object. */
type AnimationSetKey = number;
/** Minimal view of a native std::string: only its text is compared. */
type StdString = { readonly text?: string };

/**
 * Native v1.150 global std::string objects initialized by
 * CRT_InitWeaponAnimationPrefixStrings / InitGlobalAnimationSetNameStrings.
 *
 * These values are addresses of std::string OBJECTS, not enum ordinals.
 * Native animation-set maps compare the pointed-to string contents, so callers
 * may also supply an independently allocated StdString with the same text.
 */
export const DEFAULT_ANIMATION_SET_KEY: AnimationSetKey = 0x00ccccc0;
export const SWORD_ANIMATION_SET_KEY: AnimationSetKey = 0x00ccccdc;
export const SPEAR_ANIMATION_SET_KEY: AnimationSetKey = 0x00ccccf8;
export const BOW_ANIMATION_SET_KEY: AnimationSetKey = 0x00cccd14;
export const CART_ANIMATION_SET_KEY: AnimationSetKey = 0x00cccd30;
export const ONEHAND_STAFF_ANIMATION_SET_KEY: AnimationSetKey = 0x00cccd4c;
export const ONEHAND_SWORD_ANIMATION_SET_KEY: AnimationSetKey = 0x00cccd68;
export const TWOHAND_SWORD_ANIMATION_SET_KEY: AnimationSetKey = 0x00cccd84;
export const DAGGER_ANIMATION_SET_KEY: AnimationSetKey = 0x00cccda0;
export const DUAL_AXE_ANIMATION_SET_KEY: AnimationSetKey = 0x00cccdbc;
export const HARF_ANIMATION_SET_KEY: AnimationSetKey = 0x00cccdd8;
export const TWOHAND_STAFF_ANIMATION_SET_KEY: AnimationSetKey = 0x00cccdf4;
/**
 * Native weapon class 0x0a returns this second std::string object. Rizin
 * 0x008e7043 and InitGlobalAnimationSetNameStrings 0x00bbdd61..0x00bbdd84
 * prove that its content is the same `onehand_staff` name as 0x00cccd4c.
 */
export const ONEHAND_STAFF_DUPLICATE_ANIMATION_SET_KEY: AnimationSetKey = 0x00ccce10;

export const ANIMATION_SET_NAME_BY_KEY = new Map<AnimationSetKey, string>([
  [DEFAULT_ANIMATION_SET_KEY, "default"],
  [SWORD_ANIMATION_SET_KEY, "sword"],
  [SPEAR_ANIMATION_SET_KEY, "spear"],
  [BOW_ANIMATION_SET_KEY, "bow"],
  [CART_ANIMATION_SET_KEY, "cart"],
  [ONEHAND_STAFF_ANIMATION_SET_KEY, "onehand_staff"],
  [ONEHAND_SWORD_ANIMATION_SET_KEY, "onehand_sword"],
  [TWOHAND_SWORD_ANIMATION_SET_KEY, "twohand_sword"],
  [DAGGER_ANIMATION_SET_KEY, "dagger"],
  [DUAL_AXE_ANIMATION_SET_KEY, "dual_axe"],
  [HARF_ANIMATION_SET_KEY, "harf"],
  [TWOHAND_STAFF_ANIMATION_SET_KEY, "twohand_staff"],
  [ONEHAND_STAFF_DUPLICATE_ANIMATION_SET_KEY, "onehand_staff"]
]);

// Two native pointer identities carry the same comparator text. Name -> key
// stays on the first/canonical object while key -> name accepts either one.
export const ANIMATION_SET_KEY_BY_NAME = new Map<string, AnimationSetKey>();
for (const [key, name] of ANIMATION_SET_NAME_BY_KEY) {
  if (!ANIMATION_SET_KEY_BY_NAME.has(name)) {
    ANIMATION_SET_KEY_BY_NAME.set(name, key);
  }
}

/** Collapse native pointer/StdString representations to the map comparator text. */
export function animationSetNameOf(
  value: AnimationSetKey | StdString | string
): string | null {
  if (typeof value === "string") {
    return value.toLowerCase();
  }
  if (typeof value === "number") {
    return ANIMATION_SET_NAME_BY_KEY.get(value) ?? null;
  }
  return typeof value.text === "string" ? value.text.toLowerCase() : null;
}

/** Resolve authored BSR/skilleffect group text to its v1.150 global string key. */
export function animationSetKeyForName(name: string): AnimationSetKey | null {
  return ANIMATION_SET_KEY_BY_NAME.get(name.toLowerCase()) ?? null;
}
