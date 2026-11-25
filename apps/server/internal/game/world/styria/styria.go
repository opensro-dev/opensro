/*
===========================================================================

styria.go - the Styria Clash job arena: port-only identity table

Port-only, not native. The Styria Clash is the later client's job
arena (#3272): Hunters and Thieves fight for treasure in world 107
(INS_ARENA_JOB_WAR, GROUP_ARENA_JOB_WAR), on ground the v1.696 client
authors under RN_ARENA_JOB_WAR (regions 31985, 31986, 32241, 32242).
The ground arrived as a build import (scripts/styria_import.py); the
match rules are the Battle Arena engine, epic #3648, whose rules come
from the later server where it shows them.

This leaf owns only the identity table: the switch, the world, the
entry gates and the arena officers. The registration book and the
match owner live in internal/game/action (styria.go, styria_match.go).
It imports nothing, so every layer may read it.

===========================================================================
*/
package styria

import (
	"os"
	"strings"
)

// Env is the one switch the build import and the server both read
// (shardsettings.go): on builds the arena ground and runs the engine,
// off is native v1.150. The on values are the import's own set
// (scripts' FLAG_ON), so the two readers can never disagree.
const Env = "SRO_STYRIA_CLASH"

// The arena world (the v1.696 client's gameworlddata row 107:
// INS_ARENA_JOB_WAR, native type 1, one layer, no resident limit,
// group GROUP_ARENA_JOB_WAR). It joins the instance catalogue only
// while the switch is on (instance.StyriaDefinitions).
const (
	WorldID       = 107
	WorldCodeName = "INS_ARENA_JOB_WAR"
	GroupCodeName = "GROUP_ARENA_JOB_WAR"
)

// The arena regions (the client's regioncode rows under
// RN_ARENA_JOB_WAR; scripts/styria_import.py pins the same set):
// sectors x 241-242, y 124-125 of the shared world grid.
var ArenaRegions = [4]uint16{31985, 31986, 32241, 32242}

// The arena officers (the client's characterdata rows 26790 and
// 26791, TID 1/2/2, models NPC\sd\sd_arena_manager.bsr and
// NPC\sd\sd_arena_changer.bsr). The shipped v1.150 tables carry
// neither, so the roster synthesizes them from these rows while the
// switch is on (simulation's arena officer append). The manager takes
// applications (service 0x21); the exchanger is its reward neighbour
// (service 0x22) and arrives with the reward slice of #3648.
const (
	ManagerCodename   = "NPC_BATTLE_ARENA_MANAGER"
	ManagerRefObjID   = 26790
	ManagerNameSymbol = "SN_NPC_SD_ARENA_MANAGER"
	ManagerSpeech     = "SN_NPC_SD_ARENA_MANAGER_BS"
	ManagerModel      = `NPC\sd\sd_arena_manager.bsr`

	ExchangerCodename = "NPC_BATTLE_ARENA_EXCHANGER"
	ExchangerRefObjID = 26791
)

// The dialog services the manager's talk offers (the dialog lane's
// codenames, npcdialog.go). The later client's own arena UI sends
// these intents through its screens; the port's talk rows send them
// until the client slice of #3648 lands.
const (
	ServiceApply  = "STYRIA_CLASH:apply"
	ServiceCancel = "STYRIA_CLASH:cancel"
)

// The talk-row symbols. UIIT_STT_ARENA_JOB_WAR is the later client's
// own "Styria Clash" title string; the two row symbols are port-only
// and the client slice binds their text.
const (
	TitleSymbol       = "UIIT_STT_ARENA_JOB_WAR"
	ApplyTitleSymbol  = "UIIT_PORT_STYRIA_APPLY"
	CancelTitleSymbol = "UIIT_PORT_STYRIA_CANCEL"
	PromptSymbol      = "UIIT_PORT_STYRIA_PROMPT"
)

/*
================
Gate

One side's entry gate: where a transferred participant lands. The
rows are the client's teleportdata gates for world 107 (its
gameworldconfigdata names them for the arena group:
REF_TELEPORT_BATTLE_ARENA_A_GATE = GATE_HUNTER_SPAWN,
REF_TELEPORT_BATTLE_ARENA_B_GATE = GATE_THIEF_SPAWN). X and Z are
the authored landing; the height resolves on entry like every
outdoor landing.
================
*/
type Gate struct {
	CodeName string
	RegionID uint16
	X        float64
	Z        float64
}

// The two entry gates (teleportdata rows 236 and 237, world 107):
// the Hunter gate stands in region 32242 (sector 242, 125), the
// Thief gate in 31985 (sector 241, 124).
var (
	HunterGate = Gate{CodeName: "GATE_HUNTER_SPAWN", RegionID: 32242, X: 1243, Z: -1832}
	ThiefGate  = Gate{CodeName: "GATE_THIEF_SPAWN", RegionID: 31985, X: 507, Z: -1832}
)

/*
================
OfficerPlacement

One authored stand for an arena officer: a town region and the pose
the later client's npcpos rows author (region, x, y, z). The six
manager stands are the client's own placements; the port's town
regions share the client's grid, so the rows land as authored.
================
*/
type OfficerPlacement struct {
	RegionID uint16
	X        float64
	Y        float64
	Z        float64
}

// The manager's stands (the client's npcpos rows for 26790).
var ManagerPlacements = []OfficerPlacement{
	{RegionID: 25000, X: 857.58, Y: -32.58, Z: 853.56},
	{RegionID: 23687, X: 1242.4, Y: 245.95, Z: 507.35},
	{RegionID: 26265, X: 932.44, Y: -106.76, Z: 1704.04},
	{RegionID: 26959, X: 431.74, Y: 83.74, Z: 892.16},
	{RegionID: 27500, X: 346.6, Y: 180, Z: 50.77},
	{RegionID: 23088, X: 780.55, Y: 862, Z: 945.59},
}

/*
================
Enabled

Enabled reports the switch's state in this process: an on value
("1", "on", "true", any case) enables the engine, anything else is
native. It is the same reading the build import makes.
================
*/
func Enabled() bool {
	return EnabledValue(os.Getenv(Env))
}

/*
================
EnabledValue

EnabledValue is Enabled for a caller already holding the raw value
(the composition root passes the environment through once).
================
*/
func EnabledValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "on", "true":
		return true
	}
	return false
}
