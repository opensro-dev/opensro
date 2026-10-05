package guild

// v1.150 client 75F9B0/75C950 decodes result 2 and one error byte.
// The later research server uses u16 4Cxx errors; only the verified low-byte
// semantic contract is adapted here, never that later packet width.
const (
	// 5C64B9: missing guild; native v1.150 category 10:0D is silent.
	GuildErrNotMember byte = 0x0D
	// 5C64DF after 5D0F80 checks permission mask 10; 68A88C localizes.
	GuildErrPermissionDenied byte = 0x1E
	// Existing create-length contract: 5C5FB4 covers empty names.
	GuildErrInvalidGuildNameLen byte = 0x18
	// 5C64ED/5C6547 and 5C64FB/5C653B; v1.150 68A8A0/68A8AA.
	GuildErrInvalidMasterCommentTitle byte = 0x22
	GuildErrInvalidMasterComment      byte = 0x23
	// Category 0x10 rows of 68A0xx (table index + 6): the guild level-up
	// answers (0xB3F0) and the per-level member cap.
	GuildErrMemberFull     byte = 0x13
	GuildErrMemberNotFound byte = 0x1D
	// GuildErrNoCompensation is 0xB140's 0x45 (UIIT_CTL_GUILDWAR_
	// NOTCOMPENSATION, 7609C0; v1.188 0x4C45).
	GuildErrNoCompensation byte = 0x45
	// GuildErrManagerOutOfReach is CGObjPC_CheckNpcFunctionTargetInRange's
	// 3: no selected guild manager in range.
	GuildErrManagerOutOfReach byte = 0x03
	// The release vote's guards: 0x38 UIIT_MSG_MRELEASEERR_NOTSECEDE (leave,
	// 0xB56E), 0x39 _NOTREMOVE (expel, 0xB4B1), 0x33 _NOTVOTETIME (0xB6DC,
	// 760270) and v1.188 5C6AC0's raw 0x37 for a vote already running.
	GuildErrVoteNoLeave        byte = 0x38
	GuildErrVoteNoExpel        byte = 0x39
	GuildErrVoteNotTime        byte = 0x33
	GuildErrVoteRunning        byte = 0x37
	GuildErrLevelUpFull        byte = 0x21
	GuildErrLevelUpGoldDeficit byte = 0x31
	GuildErrLevelUpGPDeficit   byte = 0x32
)

// The category is supplied by the client opcode handler, not on the wire.
// Wider per-opcode branches (e.g. B663/3C) require their own encoder.
func EncodeGuildErrorResult(code byte) []byte { return []byte{2, code} }

// Not established by these constants: create duplicate/name-policy/database
// triggers, other guild mutators, direct/local dialogs and missing carriers.
// Notice client limits (127/1023) differ from the later server (128/2048).
// Do not reuse its out-of-range codes for those narrower limits without evidence.
