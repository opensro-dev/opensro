/*
===========================================================================

sro-bot - one headless bot player

The service logs in as a bot account, enters the world over the ordinary
transport and runs one scripted step: walk to a position and attack a target.
It refuses to start while the operator switch SRO_BOT_PLANE is off.

Phase 1 of issue #74 (issue #75). The bot brain arrives in phase 2. The
operator tool that starts many bots arrives in phase 3.

===========================================================================
*/
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/gamedata"
)

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// destination is a parsed -move value: region,x,y,z.
type destination struct {
	region  uint16
	x, y, z int16
}

func parseDestination(value string) (destination, error) {
	var out destination
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) != 4 {
		return out, fmt.Errorf("want region,x,y,z, got %q", value)
	}
	numbers := make([]int64, 4)
	for i, part := range parts {
		n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			return out, fmt.Errorf("part %d: %w", i, err)
		}
		numbers[i] = n
	}
	if numbers[0] < 0 || numbers[0] > 0xffff {
		return out, fmt.Errorf("region %d outside 0..65535", numbers[0])
	}
	for i := 1; i < 4; i++ {
		if numbers[i] < -32768 || numbers[i] > 32767 {
			return out, fmt.Errorf("coordinate %d outside int16", numbers[i])
		}
	}
	return destination{region: uint16(numbers[0]), x: int16(numbers[1]), y: int16(numbers[2]), z: int16(numbers[3])}, nil
}

// parsePath splits a semicolon-separated route into waypoints. The server
// navigation walks each leg; the waypoints steer around obstacles.
func parsePath(value string) ([]destination, error) {
	var out []destination
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		waypoint, err := parseDestination(part)
		if err != nil {
			return nil, err
		}
		out = append(out, waypoint)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no waypoints")
	}
	return out, nil
}

// parseLevelBands parses the -levels value: one "<level>=<region>,<x>,<y>,<z>"
// band per semicolon-separated segment. The bands are sorted by level, so the
// selection does not depend on the input order (issue #171).
func parseLevelBands(value string) ([]bot.LevelBand, error) {
	var out []bot.LevelBand
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		equals := strings.IndexByte(part, '=')
		if equals < 0 {
			return nil, fmt.Errorf("band %q: want <level>=<region>,<x>,<y>,<z>", part)
		}
		level, err := strconv.Atoi(strings.TrimSpace(part[:equals]))
		if err != nil || level < 0 {
			return nil, fmt.Errorf("band %q: bad level", part)
		}
		dest, err := parseDestination(part[equals+1:])
		if err != nil {
			return nil, fmt.Errorf("band %q: %w", part, err)
		}
		out = append(out, bot.LevelBand{
			Level: level,
			Target: bot.Point{
				Region: dest.region,
				X:      float64(dest.x),
				Y:      float64(dest.y),
				Z:      float64(dest.z),
			},
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level < out[j].Level })
	return out, nil
}

// parseRouteFile reads an authored travel route: one region,x,y,z waypoint per
// line. A blank line and a line that starts with # are skipped.
func parseRouteFile(path string) ([]bot.Point, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []bot.Point
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		dest, err := parseDestination(line)
		if err != nil {
			return nil, fmt.Errorf("line %q: %w", line, err)
		}
		out = append(out, bot.Point{
			Region: dest.region,
			X:      float64(dest.x),
			Y:      float64(dest.y),
			Z:      float64(dest.z),
		})
	}
	return out, nil
}

// parsePlanPoint parses a -plan-from/-plan-to value into a plan point. It
// shares the region,x,y,z grammar with -move and the route file.
func parsePlanPoint(value string) (bot.Point, error) {
	dest, err := parseDestination(value)
	if err != nil {
		return bot.Point{}, err
	}
	return bot.Point{
		Region: dest.region,
		X:      float64(dest.x),
		Y:      float64(dest.y),
		Z:      float64(dest.z),
	}, nil
}

// planRoute parses the endpoints and runs the route planner. The last waypoint
// is the goal. A search that finds no route reports an error.
func planRoute(pathfinder bot.Pathfinder, from, to string) ([]bot.Point, error) {
	start, err := parsePlanPoint(from)
	if err != nil {
		return nil, err
	}
	goal, err := parsePlanPoint(to)
	if err != nil {
		return nil, err
	}
	route, ok := pathfinder.Plan(start, goal)
	if !ok {
		return nil, fmt.Errorf("no route from %s to %s", from, to)
	}
	return route, nil
}

// formatRoute renders a plan as the -route file grammar so it can be piped to
// a file and fed back through -route.
func formatRoute(route []bot.Point) string {
	var out strings.Builder
	for _, point := range route {
		fmt.Fprintf(&out, "%d,%d,%d,%d\n", point.Region,
			int(math.Round(point.X)), int(math.Round(point.Y)), int(math.Round(point.Z)))
	}
	return out.String()
}

func main() {
	exitCode := 0
	// Register first so socket, recorder, and signal cleanup run before exit.
	defer func() {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}()
	log.SetFlags(0)
	log.SetPrefix("sro-bot: ")

	var (
		agentURL      = flag.String("agent", envOr("SRO_BOT_AGENT_URL", "http://127.0.0.1:8787"), "Agent base URL")
		account       = flag.String("account", envOr("SRO_BOT_ACCOUNT", ""), "bot account id")
		password      = flag.String("password", envOr("SRO_BOT_PASSWORD", ""), "bot account password")
		provision     = flag.Bool("provision", false, "create the bot account and character before the run")
		provisionOnly = flag.Bool("provision-only", false, "create the account and character, then exit (implies -provision)")
		provURL       = flag.String("provisioning-url", envOr("SRO_BOT_PROVISIONING_URL", "http://127.0.0.1:8789"), "Agent provisioning API URL")
		provToken     = flag.String("provisioning-token", envOr("SRO_BOT_PROVISIONING_TOKEN", ""), "Agent provisioning bearer token")
		provTokenFile = flag.String("provisioning-token-file", envOr("SRO_BOT_PROVISIONING_TOKEN_FILE", "/srv/opensro/opensro/apps/server/.state/cluster/agent-provisioning-token"), "file with the provisioning token")
		model         = flag.String("model", "", "character model codename for -provision")
		serverID      = flag.String("server", envOr("SRO_BOT_SERVER", "global-official"), "catalog shard id for login")
		divisionID    = flag.String("division", envOr("SRO_BOT_DIVISION", "global-official"), "division id")
		character     = flag.String("character", envOr("SRO_BOT_CHARACTER", ""), "character name")
		transportURL  = flag.String("transport", envOr("SRO_BOT_TRANSPORT", "ws://127.0.0.1:8788/transport/ws"), "transport WebSocket URL")
		moveTo        = flag.String("move", "", "walk destination region,x,y,z")
		path          = flag.String("path", "", "predefined route of waypoints region,x,y,z separated by semicolons")
		route         = flag.String("route", envOr("SRO_BOT_ROUTE", ""), "authored travel route file, one region,x,y,z per line")
		target        = flag.String("target", envOr("SRO_BOT_TARGET", ""), "navigation destination region,x,y,z; the brain plans a route from the live position")
		planFrom      = flag.String("plan-from", "", "derive a route from region,x,y,z and print it (with -plan-to)")
		planTo        = flag.String("plan-to", "", "derive a route to region,x,y,z and print it")
		planGrid      = flag.Float64("plan-grid", 64, "grid step of the route search in world units")
		planBudget    = flag.Int("plan-budget", 40000, "probe budget of the route search")
		leg           = flag.Duration("leg", 3*time.Second, "time to walk each path leg before the next waypoint")
		attackGID     = flag.Uint64("attack-gid", 0, "basic-attack target gid")
		brainOn       = flag.Bool("brain", false, "run the phase-2 brain: search, approach and fight")
		monsterRefs   = flag.String("monster-refs", envOr("SRO_BOT_MONSTER_REFS", ""), "comma-separated monster refObjIds for -brain")
		tick          = flag.Duration("tick", 500*time.Millisecond, "brain tick interval")
		attackRange   = flag.Float64("attack-range", 30, "brain melee range in world units")
		potionHP      = flag.Float64("potion-hp", 0.4, "brain drinks below this HP fraction")
		lootRange     = flag.Float64("loot-range", 200, "brain loots its own drops within this distance")
		lootFilter    = flag.String("loot", envOr("SRO_BOT_LOOT", "gold"), "loot filter: gold (default) loots gold heaps only, all also loots item drops")
		lootGrace     = flag.Int("loot-grace", 10, "hold the target choice for this many ticks after a target is lost, so the bot loots its own drop before a new monster")
		retaliate     = flag.Bool("retaliate", true, "fight the monster that most recently attacked the bot")
		aggroTicks    = flag.Int("aggressor-ticks", 20, "treat an attacker as fresh for this many brain ticks")
		targetRange   = flag.Float64("target-range", 0, "monster pick distance bound in world units (0 uses the whole region)")
		radius        = flag.Float64("radius", 500, "hunt radius in world units: roam inside it when no monster is in range (negative disables)")
		fightTicks    = flag.Int("fight-ticks", 120, "blacklist a target after this many fight ticks without a kill")
		levelGoal     = flag.Int("level", 0, "level goal: mark it met when the bot reaches this level (0 disables)")
		statBuild     = flag.String("stat", envOr("SRO_BOT_STAT", "str"), "stat that gets the level stat points: str (default), int or none")
		levels        = flag.String("levels", envOr("SRO_BOT_LEVELS", ""), "level bands: level=region,x,y,z separated by semicolons; the bot travels to the band target when it reaches the level")
		textdata      = flag.String("textdata", envOr("SRO_BOT_TEXTDATA", ""), "game-data textdata directory for the portal catalogue (empty resolves the game data)")
		maxHP         = flag.Int64("max-hp", 200, "the bot maximum HP for the potion fraction")
		potionSlot    = flag.Int("potion-slot", -1, "inventory bag slot of the HP potion (-1 disables)")
		potionType    = flag.Int("potion-type", 0, "type word of the HP potion (the item type flags)")
		seed          = flag.Int64("seed", 0, "fixed seed for a deterministic run (0 uses the clock)")
		stochastic    = flag.Bool("stochastic", false, "brain picks a bounded random target (default: the nearest)")
		navOn         = flag.Bool("nav", false, "plan routes with the server navmesh (walk around an obstacle)")
		navAuthority  = flag.String("nav-authority", envOr("SRO_BOT_NAV_AUTHORITY", ""), "world-authority directory for -nav (default: resolve the game data)")
		runFor        = flag.Duration("run-for", 0, "run the brain for this duration (0 runs until stopped)")
		telemetry     = flag.String("telemetry", envOr("SRO_BOT_TELEMETRY", ""), "JSONL telemetry file (empty disables the record)")
		telemetryMax  = flag.Int64("telemetry-max-bytes", bot.DefaultTelemetryMaxBytes, "telemetry file size cap in bytes")
		hold          = flag.Duration("hold", 0, "keep the session open after the actions")
		timeout       = flag.Duration("timeout", 20*time.Second, "admission timeout")
	)
	flag.Parse()
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	if *planFrom != "" || *planTo != "" {
		if *planFrom == "" || *planTo == "" {
			log.Fatal("-plan-from and -plan-to are required together")
		}
		dir := *navAuthority
		if dir == "" {
			paths, err := gamedata.Resolve()
			if err != nil {
				log.Fatalf("plan: resolve the game data: %v", err)
			}
			dir = paths.WorldAuthorityDir
		}
		route, err := planRoute(newPlanPathfinder(dir, *planGrid, *planBudget), *planFrom, *planTo)
		if err != nil {
			log.Fatalf("plan: %v", err)
		}
		fmt.Print(formatRoute(route))
		return
	}

	if err := bot.RequireEnabled(); err != nil {
		log.Fatalf("%v", err)
	}
	if *account == "" || *character == "" {
		log.Fatal("an account and a character are required")
	}
	if *provisionOnly {
		*provision = true
	}

	seedValue := *seed
	if seedValue == 0 {
		seedValue = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(seedValue))
	// The seed makes the pacing reproducible: every action follows a
	// deterministic dwell in 0..99 ms.
	dwell := func() time.Duration { return time.Duration(rng.Intn(100)) * time.Millisecond }

	var rec *bot.JSONLRecorder
	if *telemetry != "" {
		recorder, err := bot.NewJSONLRecorder(*telemetry, *account, *telemetryMax)
		if err != nil {
			log.Fatalf("telemetry: %v", err)
		}
		rec = recorder
		// A long run keeps its newest records: the recorder rotates at the
		// cap and does not stop (issue #140).
		rec.SetRotate(true)
		defer rec.Close()
	}
	log.Printf("starting: account=%s character=%s division=%s seed=%d", *account, *character, *divisionID, seedValue)

	ctx, cancel := context.WithTimeout(signalCtx, 2*time.Minute)
	defer cancel()

	agent := bot.AgentClient{BaseURL: *agentURL}
	passwordValue := *password
	if *provision {
		if passwordValue == "" {
			generated, err := bot.RandomPassword()
			if err != nil {
				log.Fatal(err)
			}
			passwordValue = generated
		}
		token := *provToken
		if token == "" {
			data, err := os.ReadFile(*provTokenFile)
			if err != nil {
				log.Fatalf("read provisioning token: %v", err)
			}
			token = strings.TrimSpace(string(data))
		}
		provisioner := bot.AgentProvisioner{
			ProvisioningURL:   *provURL,
			ProvisioningToken: token,
		}
		if err := provisioner.CreateAccount(ctx, *account, passwordValue); err != nil {
			log.Fatal(err)
		}
		log.Printf("account created: %s", *account)
	}
	login, err := agent.Login(ctx, *account, passwordValue, *serverID, *divisionID)
	if err != nil {
		log.Fatal(err)
	}
	division := login.DivisionID
	if division == "" {
		division = *divisionID
	}
	if *provision {
		if err := agent.CreateCharacter(ctx, login.SessionToken, division, *character, *model); err != nil {
			log.Fatal(err)
		}
		log.Printf("character created: %s", *character)
	}
	if *provisionOnly {
		log.Printf("provisioned: account=%s character=%s", *account, *character)
		return
	}
	admission, err := agent.TransportToken(ctx, login.SessionToken)
	if err != nil {
		log.Fatal(err)
	}
	socket, err := bot.DialWebSocket(ctx, *transportURL, *timeout)
	if err != nil {
		log.Fatal(err)
	}
	defer socket.Close()
	enterWorld, err := agent.EnterWorldToken(ctx, login.SessionToken, division, *character)
	if err != nil {
		log.Fatal(err)
	}
	welcome, objectRows, vitals, localPlayer, resetRegion, err := bot.Admit(ctx, socket, admission, enterWorld, division, *character, *timeout)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("in world: session=%d resumed=%v rows=%d self=%d hp=%d", welcome.SessionID, welcome.Resumed, len(objectRows), vitals.GID, vitals.HP)

	if *brainOn {
		refs, err := bot.ParseRefs(*monsterRefs)
		if err != nil {
			log.Fatal(err)
		}
		// An empty ref set is the "all" selection: ActorFromRow then matches on
		// the monster entity-id band (issue #163, #166).
		world := newBotWorld(*maxHP)
		world.setAggroWindow(time.Duration(*aggroTicks) * *tick)
		world.seedVitals(vitals)
		// The char-data packet carries the start position, so the brain knows
		// where it stands before the first move.
		if region, x, y, z, ok := bot.ParseCharacterPosition(localPlayer, vitals.GID, resetRegion); ok {
			world.setPosition(region, x, y, z)
		}
		if level, ok := bot.ParseCharacterLevel(localPlayer); ok {
			world.setLevel(level)
		}
		for _, row := range objectRows {
			if actor, ok := bot.ActorFromRow(row, refs); ok {
				world.add(actor)
				continue
			}
			if item, ok := bot.GroundItemFromRow(row, vitals.GID); ok {
				world.addGround(item)
			}
		}
		build, err := bot.ParseStatBuild(*statBuild)
		if err != nil {
			log.Fatalf("%v", err)
		}
		actions := &botActions{socket: socket, world: world, potionSlot: *potionSlot, potionType: uint16(*potionType), rec: rec, stats: bot.NewStatSpender(build)}
		var pathfinder bot.Pathfinder
		if *navOn {
			dir := *navAuthority
			if dir == "" {
				paths, err := gamedata.Resolve()
				if err != nil {
					log.Fatalf("nav: resolve the game data: %v", err)
				}
				dir = paths.WorldAuthorityDir
			}
			pathfinder = newNavPathfinder(dir)
			if pathfinder == nil {
				log.Fatal("nav: empty world-authority directory")
			}
		}
		var travelRoute []bot.Point
		if *route != "" {
			points, err := parseRouteFile(*route)
			if err != nil {
				log.Fatalf("route: %v", err)
			}
			if len(points) == 0 {
				log.Fatal("route: no waypoints")
			}
			travelRoute = points
		}
		levelBands, err := parseLevelBands(*levels)
		if err != nil {
			log.Fatalf("levels: %v", err)
		}
		var navTarget *bot.Point
		if *target != "" {
			point, err := parsePlanPoint(*target)
			if err != nil {
				log.Fatalf("target: %v", err)
			}
			navTarget = &point
		}
		var routePlanner bot.Pathfinder
		if navTarget != nil || len(levelBands) > 0 {
			dir := *navAuthority
			if dir == "" {
				paths, err := gamedata.Resolve()
				if err != nil {
					log.Fatalf("target: resolve the game data: %v", err)
				}
				dir = paths.WorldAuthorityDir
			}
			routePlanner = newPlanPathfinder(dir, *planGrid, *planBudget)
		}
		// The level bands travel between regions over the NPC portals
		// (issue #171). A missing catalogue leaves the navmesh fallback.
		var portals *bot.PortalCatalog
		if len(levelBands) > 0 {
			dir := *textdata
			if dir == "" {
				paths, err := gamedata.Resolve()
				if err != nil {
					log.Fatalf("levels: resolve the game data: %v", err)
				}
				dir = paths.TextdataDir
			}
			catalogue, err := bot.LoadPortalCatalog(dir)
			if err != nil {
				log.Printf("levels: portal catalogue: %v", err)
			} else {
				portals = catalogue
			}
		}
		runner := bot.NewBrain(bot.BrainConfig{
			Refs:           refs,
			AttackRange:    *attackRange,
			PotionHPBelow:  *potionHP,
			LootRange:      *lootRange,
			LootItems:      *lootFilter == "all",
			LootGraceTicks: *lootGrace,
			Retaliate:      *retaliate,
			AggressorTicks: *aggroTicks,
			TargetRange:    *targetRange,
			Radius:         *radius,
			FightTicks:     *fightTicks,
			LevelGoal:      *levelGoal,
			Levels:         levelBands,
			Portals:        portals,
			Pathfinder:     pathfinder,
			Route:          travelRoute,
			Target:         navTarget,
			RoutePlanner:   routePlanner,
			Stochastic:     *stochastic,
			Seed:           seedValue,
		})
		rec.Record("login", map[string]any{
			"character": *character, "division": division,
			"gid": vitals.GID, "hp": vitals.HP, "mp": vitals.MP, "rows": len(objectRows),
		})
		// A persistent bot runs until it is stopped. -run-for bounds a run.
		runCtx := signalCtx
		if *runFor > 0 {
			var cancelRun context.CancelFunc
			runCtx, cancelRun = context.WithTimeout(signalCtx, *runFor)
			defer cancelRun()
		}
		if err := runBrain(runCtx, socket, world, actions, runner, refs, *tick); err != nil && runCtx.Err() == nil {
			log.Printf("brain: %v", err)
			exitCode = 1
		}
		return
	}

	walk := func(dest destination) {
		if err := socket.Write(bot.MovementOpcode, bot.EncodeMovementTo(dest.region, dest.x, dest.y, dest.z)); err != nil {
			log.Fatal(err)
		}
		log.Printf("walk: region=%d x=%d y=%d z=%d", dest.region, dest.x, dest.y, dest.z)
		// Drain frames while the server walks this leg, so keepalive traffic
		// is answered and the next waypoint waits for the leg to finish.
		deadline := time.Now().Add(*leg)
		for time.Now().Before(deadline) {
			frame, err := socket.Read(ctx, time.Until(deadline))
			if err != nil {
				break
			}
			log.Printf("recv: opcode=0x%04X bytes=%d", frame.Opcode, len(frame.Payload))
		}
	}

	if *path != "" {
		waypoints, err := parsePath(*path)
		if err != nil {
			log.Fatalf("-path: %v", err)
		}
		for _, waypoint := range waypoints {
			time.Sleep(dwell())
			walk(waypoint)
		}
	} else if *moveTo != "" {
		dest, err := parseDestination(*moveTo)
		if err != nil {
			log.Fatalf("-move: %v", err)
		}
		time.Sleep(dwell())
		walk(dest)
	}
	if *attackGID != 0 {
		time.Sleep(dwell())
		if err := socket.Write(bot.TargetInteractOpcode, bot.EncodeBasicAttack(uint32(*attackGID))); err != nil {
			log.Fatal(err)
		}
		log.Printf("attack: gid=%d", *attackGID)
	}
	if *hold > 0 {
		// Stay connected and answer keepalive traffic. The frames the
		// server sends (move ack, attack result, kills) are logged for the
		// operator; the JSONL telemetry arrives in phase 3.
		deadline := time.Now().Add(*hold)
		for time.Now().Before(deadline) {
			frame, err := socket.Read(ctx, time.Until(deadline))
			if err != nil {
				break
			}
			log.Printf("recv: opcode=0x%04X bytes=%d", frame.Opcode, len(frame.Payload))
		}
	}
	log.Printf("done")
}

// logHeartbeatTicks is the number of ticks between two log lines of a brain
// that does not change its state or its target.
const logHeartbeatTicks = 120

// runBrain drives the brain loop: it reads frames to refresh the world view,
// then ticks the brain.
func runBrain(ctx context.Context, socket *bot.Socket, world *botWorld, actions *botActions, brain *bot.Brain, refs bot.MonsterRefs, tick time.Duration) error {
	if tick <= 0 {
		tick = 500 * time.Millisecond
	}
	rec := actions.rec
	levelGoalRecorded := false
	navFailedRecorded := false
	// lastTargetGID tracks the previous target, so the retaliate event fires
	// once when the brain switches to the aggressor (issue #185).
	lastTargetGID := uint32(0)
	// wasRoaming tracks the previous roam state, so the roam event fires once
	// when a new roam starts (issue #170).
	wasRoaming := false
	// The log gets a line on a state or target change and a heartbeat, not
	// one line per tick: the log file has no size limit (issue #140).
	lastLogState := ""
	lastLogTarget := uint32(0)
	logTicks := 0
	// despawnBracket is set between a 0x30CB byListSub=2 start and the 0x330A
	// finalize: the 0x3417 chunk in between carries raw despawn gids, not
	// spawn rows (issue #158).
	despawnBracket := false
	entry := sceneEntry{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		deadline := time.Now().Add(tick)
		for time.Now().Before(deadline) {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			frame, err := socket.Read(ctx, remaining)
			if err != nil {
				if errors.Is(err, bot.ErrReadTimeout) {
					break
				}
				return fmt.Errorf("connection ended: %w", err)
			}
			if frame.Opcode == bot.OpcodeSceneReset {
				despawnBracket = false
			}
			if handled, err := entry.handle(frame, world, brain, socket); handled || err != nil {
				if err != nil {
					return err
				}
				continue
			}
			switch frame.Opcode {
			case bot.OpcodeObjectListStart:
				// A byListSub=2 bracket opens a bulk despawn: the next 0x3417
				// chunk is a raw gid list, not spawn rows (issue #158).
				if len(frame.Payload) > 0 && frame.Payload[0] == 0x02 {
					despawnBracket = true
				}
			case bot.OpcodeObjectListFinalize:
				despawnBracket = false
			case bot.OpcodeObjectListChunk:
				if despawnBracket {
					for _, gid := range bot.DespawnGIDs(frame.Payload) {
						if brain.Target() != nil && brain.Target().GID == gid {
							rec.Record("targetDespawn", map[string]any{"target": gid})
						}
						if world.hasGround(gid) {
							rec.Record("groundDespawn", map[string]any{"gid": gid})
						}
						world.remove(gid)
					}
					break
				}
				if actor, ok := bot.ActorFromRow(frame.Payload, refs); ok {
					world.add(actor)
					break
				}
				if item, ok := bot.GroundItemFromRow(frame.Payload, world.Self().GID); ok {
					world.addGround(item)
				}
			case bot.OpcodeSingleObjectSpawn:
				if actor, ok := bot.ActorFromRow(frame.Payload, refs); ok {
					world.add(actor)
					break
				}
				if item, ok := bot.GroundItemFromRow(frame.Payload, world.Self().GID); ok {
					world.addGround(item)
				}
			case bot.OpcodeObjectDespawn:
				if gid, ok := bot.DespawnGID(frame.Payload); ok {
					// A despawn is a scene-visibility event, not an
					// authoritative combat or loot confirmation: a target can
					// leave view or be killed by another player, and a drop can
					// be granted to another player.
					if brain.Target() != nil && brain.Target().GID == gid {
						rec.Record("targetDespawn", map[string]any{"target": gid})
					}
					if world.hasGround(gid) {
						rec.Record("groundDespawn", map[string]any{"gid": gid})
					}
					world.remove(gid)
				}
			case bot.OpcodeMovementAck:
				if ack, ok := bot.ParseMovementAck(frame.Payload); ok {
					selfGID := world.Self().GID
					world.setAck(ack)
					if selfGID != 0 && ack.GID == selfGID {
						rec.Record("moveAck", map[string]any{
							"region": ack.Region, "x": ack.X, "y": ack.Y, "z": ack.Z,
							"gid": ack.GID, "self": selfGID, "hasSource": ack.HasSource,
							"latencyMs": time.Since(actions.moveSent).Milliseconds(),
						})
					}
				}
			case bot.OpcodeSourceCorrection:
				// The 0xB2F5 source correction carries the TRUE live position,
				// unlike the 0xB738 ack (the leg goal). Snap to it and cancel
				// the interpolation leg.
				if correction, ok := bot.ParseSourceCorrection(frame.Payload); ok {
					selfGID := world.Self().GID
					world.setCorrection(correction)
					if selfGID != 0 && correction.GID == selfGID {
						rec.Record("sourceCorrection", map[string]any{
							"region": correction.Region, "x": correction.X,
							"y": correction.Y, "z": correction.Z,
						})
					}
				}
			case bot.OpcodeSkillCastResult:
				if code, ok := bot.RefusalCode(frame.Payload); ok {
					rec.Record("refusal", map[string]any{
						"channel": "attack", "code": code,
						"latencyMs": time.Since(actions.attackSent).Milliseconds(),
					})
					break
				}
				// A monster's attack publishes the 0xB245 cast result to
				// nearby sessions with the monster as the caster and the bot
				// as the target. Record it as the aggressor, so the brain
				// fights it back (issue #185).
				if aggressor, ok := bot.AttackerFromSkillCast(frame.Payload, world.Self().GID); ok {
					world.setAggro(aggressor)
				}
			case bot.OpcodeExpUpdate:
				// The 0x30D2 experience update is authoritative: the server
				// sends it only to the character that earned the experience.
				// A monster source with a positive delta proves that this bot
				// killed the target (issue #175).
				update, ok := bot.ParseExpUpdate(frame.Payload)
				if ok && update.Levelled {
					// The level-up tail carries the absolute remaining stat
					// points (issue #200).
					actions.stats.SetPool(int(update.StatPoints))
					rec.Record("statPoints", map[string]any{
						"points": update.StatPoints, "source": "expUpdate",
					})
				}
				if ok && update.IsMonsterKill() {
					rec.Record("kill", map[string]any{
						"target": update.SourceGID, "exp": update.ExpDelta,
						"skillExp": update.SkillExpDelta, "leveled": update.Levelled,
						"gid": actions.lastAttackGID, "matched": actions.lastAttackGID == update.SourceGID,
					})
				}
			case bot.OpcodeItemMoveResponse:
				if code, ok := bot.RefusalCode(frame.Payload); ok {
					rec.Record("refusal", map[string]any{"channel": "pickup", "code": code})
					break
				}
				// The 0xB06D pickup grant is authoritative: the server sends
				// it to the character that received the drop (issue #175).
				if grant, ok := bot.ParsePickupGrant(frame.Payload); ok {
					rec.Record("lootGrant", map[string]any{
						"gold": grant.Gold, "amount": grant.Amount, "slot": grant.Slot,
						"gid": actions.lastPickupGID, "matched": actions.lastPickupGID != 0,
					})
				}
			case bot.OpcodePointsUpdate:
				// The 0x30B3 type-3 update is the absolute remaining stat
				// points (issue #200).
				if points, ok := bot.ParsePointsStat(frame.Payload); ok {
					actions.stats.SetPool(points)
					rec.Record("statPoints", map[string]any{
						"points": points, "source": "pointsUpdate",
					})
				}
			case bot.OpcodeAllocStrAck, bot.OpcodeAllocIntAck:
				if spent, ok := actions.stats.Ack(frame.Payload); ok {
					rec.Record("stat", map[string]any{
						"stat": bot.StatName(actions.stats.Build()), "spent": spent,
						"remaining": actions.stats.Pool(),
					})
				}
			case bot.OpcodeVitalsUpdate:
				if v, ok := bot.ParseVitals(frame.Payload); ok {
					world.setVitals(v)
				}
			case bot.OpcodeBaseStats:
				// The base-stat block carries the real maximum HP. The
				// server sends it at a level-up and at a stat or equipment
				// change (issue #140).
				if maxHP, ok := bot.ParseBaseStatsMaxHP(frame.Payload); ok {
					world.setMaxHP(maxHP)
				}
			case bot.OpcodeLevelUpEffect:
				if gid, ok := bot.LevelUpGID(frame.Payload); ok {
					selfGID := world.Self().GID
					if selfGID != 0 && gid == selfGID {
						world.addLevel()
						rec.Record("levelUp", map[string]any{"level": world.Self().Level})
					}
				}
			case bot.OpcodeObjectStateRefresh:
				if life, ok := bot.ParseLife(frame.Payload); ok {
					selfGID := world.Self().GID
					wasDead := world.Self().Dead
					world.setLife(life)
					if selfGID != 0 && life.GID == selfGID && life.Dead && !wasDead {
						rec.Record("death", map[string]any{"hp": world.Self().HP})
					}
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.pending {
			if time.Since(entry.started) >= 20*time.Second {
				return fmt.Errorf("scene re-entry timeout")
			}
			continue
		}
		if err := brain.Tick(world, actions); err != nil {
			return err
		}
		self := world.Self()
		// Publish the spender's tracked pool on the self view, so the
		// telemetry and the decision line report one value (issue #200).
		self.StatPoints = actions.stats.Pool()
		// Spend one level stat point per acknowledgement (issue #200).
		if !self.Dead {
			if opcode, send := actions.stats.Next(); send {
				if err := socket.Write(opcode, nil); err != nil {
					return err
				}
			}
		}
		target := uint32(0)
		if brain.Target() != nil {
			target = brain.Target().GID
		}
		if target != 0 && target == self.Aggressor && target != lastTargetGID {
			// The brain switched to the attacker: record the reaction
			// (issue #185).
			rec.Record("retaliate", map[string]any{"aggressor": target})
		}
		lastTargetGID = target
		// The roam event fires once when a new roam starts (issue #170).
		roaming := brain.Roaming()
		if roaming && !wasRoaming {
			if point := brain.RoamTarget(); point != nil {
				rec.Record("roam", map[string]any{
					"region": point.Region, "x": point.X, "y": point.Y, "z": point.Z,
				})
			}
		}
		wasRoaming = roaming
		// The portal event fires once per gate request (issue #171).
		if event := brain.TakePortalEvent(); event != nil {
			rec.Record("portal", map[string]any{
				"gate":        event.GateGID,
				"destination": event.DestinationID,
				"fromRegion":  event.FromRegion,
				"toRegion":    event.ToRegion,
			})
		}
		rec.Record("decision", map[string]any{
			"state": brain.State().String(), "target": target,
			"monsters": len(world.Monsters()),
			"region":   self.Region, "x": self.X, "y": self.Y, "z": self.Z,
			"hp": self.HP, "mp": self.MP, "dead": self.Dead, "level": self.Level,
			// aggressor is the monster that attacked the bot, or 0
			// (issue #185).
			"aggressor": self.Aggressor,
			// statPoints is the tracked remaining stat-point pool, set from
			// the last absolute update (issue #200).
			"statPoints": self.StatPoints,
			// roam reports that the brain roams inside the hunt radius
			// (issue #170).
			"roam": roaming,
			// nav explains a travel hold (issue #143, F2): the reason, the
			// position status, the route length, the remaining arrival time
			// and the time since the last confirmed progress.
			"nav": brain.NavStatus(), "recovery": brain.RecoveryReason(),
		})
		if brain.LevelGoalMet() && !levelGoalRecorded {
			levelGoalRecorded = true
			rec.Record("levelGoal", map[string]any{"level": self.Level})
		}
		if brain.NavFailed() && !navFailedRecorded {
			navFailedRecorded = true
			rec.Record("navFailed", map[string]any{"reason": brain.NavStatus().HoldReason})
		}
		logTicks++
		if state := brain.State().String(); state != lastLogState || target != lastLogTarget || logTicks >= logHeartbeatTicks {
			lastLogState, lastLogTarget, logTicks = state, target, 0
			log.Printf("brain: state=%s target=%d monsters=%d hp=%d dead=%v", state, target, len(world.Monsters()), self.HP, self.Dead)
		}
	}
}

// botWorld is the brain's live world view.
type botWorld struct {
	mu          sync.Mutex
	self        bot.Self
	monsters    map[uint32]bot.Actor
	groundItems map[uint32]bot.GroundItem

	// aggressorGID and aggressorAtMs record the monster that most recently
	// attacked the bot. Self() exposes the gid only while the hit is fresh
	// within aggressorWindowMs (issue #185).
	aggressorGID      uint32
	aggressorAtMs     int64
	aggressorWindowMs int64
	// frame is the fixed seed-local world frame (the bot's start region). The
	// live position is interpolated in this frame, so a leg across a region
	// boundary stays continuous instead of snapping to the goal.
	frame    bot.WorldFrame
	frameSet bool
	// moveGoal is the committed destination of the move in flight, moveFrom the
	// reckoned live position when the move was sent (both in the seed-local
	// world frame), moveFromMs the time. The destination acknowledgement
	// carries the goal, not the live position, so the world reckons the
	// position from the leg. Without it the distance to a target flips between
	// the goal and the real position and the brain thrashes between fight and
	// approach.
	moveGoal   *simulation.Vec3
	moveFrom   simulation.Vec3
	moveFromMs int64
	// pendingMove reports that a move is outstanding. The brain sends one leg
	// and waits for the arrival, so an acknowledgement that arrives with no
	// open leg is a stale or foreign frame and must not re-anchor the estimate.
	// The drift fix (issue #148): without it a foreign ack pulled the estimate
	// at walk speed while the character stood, and the fight/approach decision
	// flipped on the drifting distance.
	pendingMove bool
}

func newBotWorld(maxHP int64) *botWorld {
	return &botWorld{
		monsters:    map[uint32]bot.Actor{},
		groundItems: map[uint32]bot.GroundItem{},
		self:        bot.Self{HP: maxHP, MaxHP: maxHP},
	}
}

// defaultAggressorWindowMs is the fallback freshness window of a recorded
// aggressor: 20 ticks at the 500 ms default tick (issue #185). The service
// overrides it from -aggressor-ticks and -tick.
const defaultAggressorWindowMs int64 = 20 * 500

// setAggro records the monster that most recently attacked the bot and the
// wall clock at the hit (issue #185). Self() exposes the gid only while it is
// fresh.
func (w *botWorld) setAggro(gid uint32) {
	w.mu.Lock()
	w.aggressorGID = gid
	w.aggressorAtMs = time.Now().UnixMilli()
	w.mu.Unlock()
}

// setAggroWindow sets how long Self() exposes a recorded aggressor. The
// service derives the window from -aggressor-ticks and -tick (issue #185).
func (w *botWorld) setAggroWindow(window time.Duration) {
	w.mu.Lock()
	w.aggressorWindowMs = window.Milliseconds()
	w.mu.Unlock()
}

func (w *botWorld) Self() bot.Self {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now().UnixMilli()
	self := w.self
	if w.moveGoal != nil {
		live := w.liveLocked(now)
		self.Region = live.RegionID
		self.X, self.Y, self.Z = live.X, live.Y, live.Z
	}
	// Expose the aggressor only while the hit is fresh (issue #185). A stale
	// gid is dropped, so the brain stops preferring a monster that has not
	// attacked the bot for a while.
	if w.aggressorGID != 0 {
		window := w.aggressorWindowMs
		if window <= 0 {
			window = defaultAggressorWindowMs
		}
		if now-w.aggressorAtMs <= window {
			self.Aggressor = w.aggressorGID
		} else {
			w.aggressorGID = 0
		}
	}
	self.NowMs = now
	return self
}

// liveLocked reckons the live position along the committed leg in the world
// frame. The destination acknowledgement carries the goal, not the live
// position, so the world interpolates from the leg start toward the goal over
// the estimated travel time and resolves the region and locals from the
// interpolated world point. A leg that crosses a region boundary therefore
// stays continuous and does not snap to the goal.
func (w *botWorld) liveLocked(now int64) simulation.Spawn {
	if w.moveGoal == nil {
		return simulation.Spawn{RegionID: w.self.Region, X: w.self.X, Y: w.self.Y, Z: w.self.Z}
	}
	goal := *w.moveGoal
	from := w.moveFrom
	dist := math.Hypot(goal.X-from.X, goal.Z-from.Z)
	if dist <= 0 {
		return w.frame.FromWorld(goal)
	}
	travelMs := dist / simulation.WalkSpeed * 1000
	elapsed := float64(now - w.moveFromMs)
	if elapsed >= travelMs {
		return w.frame.FromWorld(goal)
	}
	if elapsed < 0 {
		elapsed = 0
	}
	return w.frame.FromWorld(bot.InterpolateWorld(from, goal, elapsed/travelMs))
}

func (w *botWorld) Monsters() []bot.Actor {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]bot.Actor, 0, len(w.monsters))
	for _, m := range w.monsters {
		out = append(out, m)
	}
	return out
}

func (w *botWorld) GroundItems() []bot.GroundItem {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]bot.GroundItem, 0, len(w.groundItems))
	for _, g := range w.groundItems {
		out = append(out, g)
	}
	return out
}

func (w *botWorld) add(actor bot.Actor) {
	w.mu.Lock()
	w.monsters[actor.GID] = actor
	w.mu.Unlock()
}

func (w *botWorld) addGround(item bot.GroundItem) {
	w.mu.Lock()
	w.groundItems[item.GID] = item
	w.mu.Unlock()
}

// hasGround reports whether gid is a tracked ground item, so a despawn can be
// recorded as a ground despawn.
func (w *botWorld) hasGround(gid uint32) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.groundItems[gid]
	return ok
}

func (w *botWorld) remove(gid uint32) {
	w.mu.Lock()
	delete(w.monsters, gid)
	delete(w.groundItems, gid)
	w.mu.Unlock()
}

// clear drops the view on a scene reset (a region change), so a monster of the
// old region cannot stay a target.

// setAck commits the destination of the bot's own movement acknowledgement as
// the GOAL of the leg. The server clips a blocked move, so this is where the
// character actually stopped, not the requested target. The live position is
// interpolated toward this goal in the world frame; a separate 0xB2F5 source
// correction snaps the position authoritatively.
func (w *botWorld) setAck(ack bot.MovementAck) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.self.GID == 0 || ack.GID != w.self.GID {
		return
	}
	// Only an acknowledgement for the open leg may re-anchor the estimate. A
	// stale or foreign ack (an earlier leg, or another entity) would pull the
	// position away from the character (issue #148).
	if !w.pendingMove {
		return
	}
	w.pendingMove = false
	w.ensureFrameLocked(ack.Region)
	now := time.Now().UnixMilli()
	// The optional source block is the true position the move started from.
	// Apply it as the authoritative live position before the leg (issue #150).
	if ack.HasSource {
		w.self.Region = ack.SourceRegion
		w.self.X = ack.SourceX
		w.self.Y = ack.SourceY
		w.self.Z = ack.SourceZ
		w.self.PositionKnown = true
		w.moveGoal = nil
	}
	// The ack is the committed destination, not the live position. Reckon the
	// leg from the current live position toward it.
	goal := simulation.Spawn{RegionID: ack.Region, X: ack.X, Y: ack.Y, Z: ack.Z}
	goalWorld := w.frame.ToWorld(goal)
	if w.self.PositionKnown {
		w.moveFrom = w.frame.ToWorld(w.liveLocked(now))
	} else {
		// Without a known start the leg cannot be interpolated; commit the
		// goal as both ends so the estimate is the goal immediately.
		w.moveFrom = goalWorld
	}
	w.moveGoal = &goalWorld
	w.moveFromMs = now
	w.self.PositionKnown = true
}

// noteMoveRequest marks that a move is outstanding, so the next own movement
// acknowledgement may re-anchor the estimate (issue #148).
func (w *botWorld) noteMoveRequest() {
	w.mu.Lock()
	w.pendingMove = true
	w.mu.Unlock()
}

// freeze commits the current estimate as the standing position and clears the
// interpolation leg. The brain calls it while the character does not walk
// (attack, pickup), so the estimate stops running at walk speed and the
// fight/approach distance is stable (issue #148).
func (w *botWorld) freeze() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.moveGoal == nil {
		return
	}
	live := w.liveLocked(time.Now().UnixMilli())
	w.self.Region = live.RegionID
	w.self.X, w.self.Y, w.self.Z = live.X, live.Y, live.Z
	w.moveGoal = nil
	w.pendingMove = false
}

// ensureFrameLocked fixes the seed region of the world frame on first use. The
// char-data packet sets it to the bot's start region; this is the fallback when
// a movement frame arrives first.
func (w *botWorld) ensureFrameLocked(region uint16) {
	if !w.frameSet {
		w.frame = bot.WorldFrame{SeedRegion: region}
		w.frameSet = true
	}
}

// setCorrection applies the server's authoritative source position (0xB2F5)
// for the bot's own object. It cancels the interpolation leg and snaps the live
// position to the corrected point: this is the convergence path after a server
// correction (a blocked or resteered move). The 0xB738 ack is only the leg
// GOAL, so it cannot serve this role.
func (w *botWorld) setCorrection(correction bot.Correction) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.self.GID == 0 || correction.GID != w.self.GID {
		return
	}
	w.self.Region = correction.Region
	w.self.X = correction.X
	w.self.Y = correction.Y
	w.self.Z = correction.Z
	w.self.PositionKnown = true
	w.moveGoal = nil
	w.pendingMove = false
}

// setPosition seeds the self position from the char-data packet at enter-world.
func (w *botWorld) setPosition(region uint16, x, y, z float64) {
	w.mu.Lock()
	w.self.Region = region
	w.self.X = x
	w.self.Y = y
	w.self.Z = z
	w.self.PositionKnown = true
	w.frame = bot.WorldFrame{SeedRegion: region}
	w.frameSet = true
	w.moveGoal = nil
	w.pendingMove = false
	w.mu.Unlock()
}

// setLevel seeds the self level from the char-data packet at enter-world.
func (w *botWorld) setLevel(level int) {
	w.mu.Lock()
	w.self.Level = level
	w.mu.Unlock()
}

// addLevel raises the self level on a level-up presentation.
func (w *botWorld) addLevel() {
	w.mu.Lock()
	w.self.Level++
	w.mu.Unlock()
}

// setVitals applies a 0x33A6 HP/MP refresh. The server sends this frame for
// other entities too (monsters, peer summons), so a foreign GID must never
// replace the bot's own object id: setAck would then adopt that entity's
// movement acknowledgements and the estimate would drift (issue #148). The
// admission frame seeds the id.
func (w *botWorld) setVitals(v bot.Vitals) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.self.GID == 0 {
		w.self.GID = v.GID
	}
	if v.GID != w.self.GID {
		return
	}
	w.self.HP = v.HP
	w.self.MP = v.MP
	// The maximum is not below a current value the server reported. This
	// holds the potion threshold until the first base-stat block arrives.
	if v.HP > w.self.MaxHP {
		w.self.MaxHP = v.HP
	}
}

// setMaxHP applies the maximum HP of the base-stat block (issue #140).
func (w *botWorld) setMaxHP(maxHP int64) {
	w.mu.Lock()
	w.self.MaxHP = maxHP
	w.mu.Unlock()
}

// seedVitals seeds the self view from the admission vitals refresh. That frame
// belongs to the bot, so it sets the object id.
func (w *botWorld) seedVitals(v bot.Vitals) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if v.GID != 0 {
		w.self.GID = v.GID
	}
	w.self.HP = v.HP
	w.self.MP = v.MP
	if v.HP > w.self.MaxHP {
		w.self.MaxHP = v.HP
	}
}

// setLife applies a 0x3122 life refresh for the bot's own character.
func (w *botWorld) setLife(life bot.Life) {
	w.mu.Lock()
	if w.self.GID == 0 {
		w.self.GID = life.GID
	}
	if life.GID != w.self.GID {
		w.mu.Unlock()
		return
	}
	w.self.Dead = life.Dead
	w.mu.Unlock()
}

// botActions sends the brain's actions over the socket.
type botActions struct {
	socket     *bot.Socket
	world      *botWorld
	potionSlot int
	potionType uint16
	rec        *bot.JSONLRecorder
	moveSent   time.Time
	attackSent time.Time
	// lastAttackGID and lastPickupGID are the last target gids the brain
	// attacked and requested. The kill and lootGrant telemetry pairs them
	// with the authoritative packet for a best-effort match (issue #175).
	lastAttackGID uint32
	lastPickupGID uint32
	// stats spends the level stat points (issue #200). Nil disables it.
	stats *bot.StatSpender
}

func (a *botActions) MoveTo(region uint16, x, y, z float64) error {
	mx, my, mz := worldToMove(x, y, z)
	a.moveSent = time.Now()
	a.world.noteMoveRequest()
	a.rec.Record("move", map[string]any{"region": region, "x": mx, "y": my, "z": mz})
	// The committed position comes back in the movement acknowledgement, not
	// from this requested destination: the server clips a blocked move.
	return a.socket.Write(bot.MovementOpcode, bot.EncodeMovementTo(region, mx, my, mz))
}

// Pickup sends the 0x72CD ground-item request. The server owns the travel and
// grants the drop on arrival, so a repeat is idempotent.
func (a *botActions) Pickup(gid uint32) error {
	a.world.freeze()
	a.lastPickupGID = gid
	a.rec.Record("loot", map[string]any{"gid": gid})
	return a.socket.Write(bot.TargetInteractOpcode, bot.EncodePickup(gid))
}

// Select sends the 0x745A object-selection request for the gate NPC. The
// server records the selection the portal request then consumes (issue #171).
func (a *botActions) Select(gid uint32) error {
	a.rec.Record("select", map[string]any{"gid": gid})
	return a.socket.Write(bot.OpObjectSelectRequest, bot.EncodeObjectSelect(gid))
}

// Portal sends the 0x7495 gate-travel request [u32 gid][u8 kind=2][u32
// destination] (issue #171).
func (a *botActions) Portal(gid, destinationID uint32) error {
	a.rec.Record("portalRequest", map[string]any{"gate": gid, "destination": destinationID})
	return a.socket.Write(bot.PortalOpcode, bot.EncodePortal(gid, destinationID))
}

func (a *botActions) Attack(gid uint32) error {
	a.world.freeze()
	a.attackSent = time.Now()
	a.lastAttackGID = gid
	a.rec.Record("attack", map[string]any{"target": gid})
	return a.socket.Write(bot.TargetInteractOpcode, bot.EncodeBasicAttack(gid))
}

func (a *botActions) UsePotion() error {
	if a.potionSlot < 0 {
		return bot.ErrRecoveryUnavailable
	}
	a.rec.Record("potion", map[string]any{"slot": a.potionSlot})
	return a.socket.Write(bot.OpcodeItemUseRequest, bot.EncodeItemUse(uint8(a.potionSlot), a.potionType))
}

// Respawn sends the 0x32DC self-rebirth request at the present point. The
// server refuses the request when the character is alive, so a repeat is
// harmless until the LIFE-alive refresh arrives.
func (a *botActions) Respawn() error {
	level := a.world.Self().Level
	payload := bot.EncodeRebirth(level)
	a.rec.Record("respawn", map[string]any{"level": level, "choice": payload[0]})
	return a.socket.Write(bot.OpcodeLocalRebirthRequest, payload)
}

// worldToMove converts a brain position to the 0x7738 payload fields. The
// brain and the object-list rows share the server's region-local frame, so
// the wire x/z are the same coordinates rounded to whole units. An earlier
// xSROMap-style sector/scale conversion treated the row as a global map
// coordinate and sent the server a destination far outside the region; the
// server then refused or misplaced the move, so the bot only killed in reach.
func worldToMove(x, y, z float64) (int16, int16, int16) {
	return int16(math.Round(x)), int16(math.Round(y)), int16(math.Round(z))
}
