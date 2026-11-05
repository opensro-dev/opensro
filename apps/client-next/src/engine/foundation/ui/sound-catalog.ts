// Active v1.150 effectsound.txt UI rows; native placeholders do not invent triggers.
export function createUiSoundCatalog() {
	return {
		// Non-positional ITEM interactions share UI prewarming. The animation
		// owns pickup playback; preloading prevents its first cue expiring cold.
		SND_PICKUP: [ { path: "/assets/audio/sfx/prim/snd/ui/itpickup.wav", gain: 1 } ],
		// ITEM:SND_DROPITEM:-:GOLD:-:-; 8F9690 also passes null position.
		SND_DROPITEM: [ { path: "/assets/audio/sfx/prim/snd/ui/itgold.wav", gain: 1 } ],
		// CIFPlayerMiniInfo 6B6D00 requests this at the actor position, distance scale 1.
		SND_ALARM: [ { path: "/assets/audio/sfx/prim/snd/ui/alarm_sound.wav", gain: 1 } ],
		"SND_BUTTON_CLICK": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/uibutton_a.wav",
				"gain": 0.8
			},
			{
				"path": "/assets/audio/sfx/prim/snd/ui/uibutton_b.wav",
				"gain": 0.8
			}
		],
		"SND_WINDOW_OPEN": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/uiwinopen.wav",
				"gain": 0.8
			}
		],
		"SND_WINDOW_CLOSE": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/uiwinclose.wav",
				"gain": 0.8
			}
		],
		"SND_ERROR": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/error.wav",
				"gain": 0.8
			}
		],
		"SND_REPAIR": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/itrepair_a.wav",
				"gain": 0.8
			},
			{
				"path": "/assets/audio/sfx/prim/snd/ui/itrepair_b.wav",
				"gain": 0.8
			},
			{
				"path": "/assets/audio/sfx/prim/snd/ui/itrepair_c.wav",
				"gain": 0.8
			}
		],
		"SND_REVIVE": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/itrevive.wav",
				"gain": 0.8
			}
		],
		"SND_WARP": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/gatein.wav",
				"gain": 1
			}
		],
		"SND_HYAN": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/hyanget.wav",
				"gain": 0.8
			}
		],
		"SND_POTION": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/itpotiondrink.wav",
				"gain": 0.8
			}
		],
		"SND_LEVUP": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/itlevelup.wav",
				"gain": 0.8
			}
		],
		"SND_QUEST": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/questopen.wav",
				"gain": 0.8
			}
		],
		"SND_QUEST_END": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/itquest.wav",
				"gain": 0.8
			}
		],
		"SND_WARNING": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/biff.wav",
				"gain": 0.8
			}
		],
		"SND_EQDANGER": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/itembreak.wav",
				"gain": 0.8
			}
		],
		"SND_EQBREAK": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/itemdanger.wav",
				"gain": 0.8
			}
		],
		"SND_ELIXIR_USE": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/elixir_use.wav",
				"gain": 0.8
			}
		],
		"SND_ELIXIR_SUCCESS": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/elixir_suc.wav",
				"gain": 0.8
			}
		],
		"SND_ELIXIR_FAILURE": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/elixir_fail.wav",
				"gain": 0.8
			}
		],
		"SND_ELIXIR_DESTROY": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/elixir_des.wav",
				"gain": 0.8
			}
		],
		"SND_COS_SUMMON": [
			{
				"path": "/assets/audio/sfx/prim/snd/skill/csk_heal_ready_a.wav",
				"gain": 0.6
			}
		],
		"SND_GACHA_MOVE": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/gacha_move.wav",
				"gain": 0.8
			}
		],
		"SND_GACHA_TURN": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/gacha_turn.wav",
				"gain": 0.8
			}
		],
		"SND_GACHA_END": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/gacha_end.wav",
				"gain": 0.8
			}
		],
		"SND_GACHA_WIN": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/gacha_win.wav",
				"gain": 0.8
			}
		],
		"SND_GACHA_CHANGE": [
			{
				"path": "/assets/audio/sfx/prim/snd/ui/gacha_change.wav",
				"gain": 0.8
			}
		]
	} as const;
}
export type UiSoundHandle = keyof ReturnType<typeof createUiSoundCatalog>;
