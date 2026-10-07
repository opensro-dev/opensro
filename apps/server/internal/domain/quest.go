package domain

// ActiveQuestRecord is one persisted in-progress quest. Optional fields are
// emitted by the quest wire layer according to Flags, so zero values remain
// meaningful when their flag is present.
type ActiveQuestRecord struct {
	// Stage belongs to the server quest owner. It is persisted but never added
	// to the versioned SQuestInfo wire grammar. Legacy records begin at zero.
	Stage uint16 `json:"stage,omitempty"`
	// RemainingMinutes is native quest-user +4, decremented by the online
	// character's minute pulse. Zero with a timed definition means expired.
	RemainingMinutes uint8 `json:"remainingMinutes,omitempty"`
	// ToolStep is native quest-user +0 for a tool quest whose steps run in
	// order: Rahid 5's peaks cleared. 922DE0 starts it at 1 and 8A0970 adds
	// one per success; it is stored less one, so legacy records begin at 0.
	ToolStep uint8 `json:"toolStep,omitempty"`
	// Branch is the offer reply a branching quest was accepted with (Rahid
	// 2's feathers or waiting, native quest-user +4); WaitMinutes is a waiting
	// branch's remaining online minutes (+0xC) and WaitAchieved its achieved
	// word (+0xA), set on the minute after the count reaches zero (89E050).
	Branch       uint8  `json:"branch,omitempty"`
	WaitMinutes  uint16 `json:"waitMinutes,omitempty"`
	WaitAchieved bool   `json:"waitAchieved,omitempty"`
	// SideTalksHeard has bit i set once the quest's side talk i was spoken.
	// Native quest-user +2 keeps the inverse (a set bit is pending, 89FDA0),
	// so legacy records begin with every line pending.
	SideTalksHeard uint8  `json:"sideTalksHeard,omitempty"`
	RefID          uint32 `json:"refId"`
	U08            uint8  `json:"u08"`
	U09            uint8  `json:"u09"`
	Flags          uint8  `json:"flags"`

	Progress uint32 `json:"progress,omitempty"`
	U10      uint8  `json:"u10,omitempty"`

	Contents  []ActiveQuestContentsNode `json:"contents,omitempty"`
	TargetIds []uint32                  `json:"targetIds,omitempty"`
}

// ActiveQuestContentsNode is one tagged quest-content node. A sentinel count
// is distinct from an empty objective list on the native wire.
type ActiveQuestContentsNode struct {
	// CompletionReached is the server mission latch (native quest-user +3).
	// It survives collection loss/reacquisition and is never added to SQuestInfo.
	CompletionReached bool   `json:"completionReached,omitempty"`
	Tag               uint8  `json:"tag"`
	Kind              uint8  `json:"kind"`
	Description       string `json:"description"`

	ObjectiveSentinel bool     `json:"objectiveSentinel,omitempty"`
	ObjectiveValues   []uint32 `json:"objectiveValues,omitempty"`
}

// TrackedQuestRecord is one persisted tracker row. Tail6 is normalized to six
// bytes by the quest wire encoder, and Optional is emitted when Flags bit 1 is
// set.
type TrackedQuestRecord struct {
	RefID  uint32 `json:"refId"`
	Flags  uint8  `json:"flags"`
	ValueA uint8  `json:"valueA"`
	Word   uint16 `json:"word"`

	Tail6    []uint8 `json:"tail6"`
	Optional uint32  `json:"optional,omitempty"`
}
