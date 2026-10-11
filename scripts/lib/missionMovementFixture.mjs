// Deterministic Mission movement fixtures.
//
// Route discovery and server-acceptance exploration belong to certification
// probes. Performance benchmarks consume only the immutable routes below:
// reset before browser boot, verify without input, dispatch once, then measure.

import {
  fetchProbeSessionJson,
  openProbeAgentSession,
  ProbeSessionHttpError,
  readProbeCharacterSpawnFromSession
} from "./probeSession.mjs";
import { assertCharacterAllowed } from "./probeCharacter.mjs";

export const NATIVE_MOVE_REQUEST_OPCODE = 0x7738;
export const NATIVE_MOVE_ACK_OPCODE = 0xb738;
export const BENCHMARK_FIXTURE_RESET_PATH = "/development/benchmark-fixture/reset";
export const BENCHMARK_FIXTURE_CHARACTER_CREATE_PATH = "/character/create";

const VIEWPORT = Object.freeze({ width: 1600, height: 900, deviceScaleFactor: 1 });

export const MISSION_MOVEMENT_FIXTURES = Object.freeze({
  movement: Object.freeze({
    id: "europe-field-ordinary-run-v1",
    workload: "movement",
		movementMode: 3,
    viewport: VIEWPORT,
    approach: Object.freeze({ regionId: 0x6e4a, x: 1656, y: 7, z: 525 }),
    start: Object.freeze({ regionId: 0x6e4a, x: 1756, y: 7, z: 525 }),
    startToleranceNative: 3,
    startYawRadians: 1.5708202488148315,
    startYawToleranceRadians: 0.002,
    camera: Object.freeze({
      initial: Object.freeze({
        yaw: 1.5708202956104422,
        pitch: 0.17453292519943295,
        distance: 80
      }),
      setup: Object.freeze({ startX: 0.5, startY: 0.45, deltaX: 0.16, steps: 8 }),
      measured: Object.freeze({
        yaw: 2.8508202670002127,
        pitch: 0.17453292519943295,
        distance: 80
      }),
      tolerance: 0.015
    }),
    clickFraction: Object.freeze({ x: 0.82, y: 0.52 }),
    destination: Object.freeze({ regionId: 0x6e4a, x: 1635.336, y: -28.685, z: 233.235 }),
    destinationToleranceNative: 5,
    minimumMeasuredTravelNative: 25
  }),
  region_cross: Object.freeze({
    id: "europe-field-east-boundary-cross-v1",
    workload: "region_cross",
		movementMode: 3,
    viewport: VIEWPORT,
    approach: Object.freeze({ regionId: 0x6e4b, x: 1655.2, y: 9.34, z: 527.43 }),
    start: Object.freeze({ regionId: 0x6e4b, x: 1755.2, y: 9.34, z: 527.43 }),
    startToleranceNative: 4,
    startYawRadians: 1.5708202488148315,
    startYawToleranceRadians: 0.002,
    camera: Object.freeze({
      initial: Object.freeze({
        yaw: 1.5708202956104422,
        pitch: 0.17453292519943295,
        distance: 80
      }),
      measured: Object.freeze({
        yaw: 1.5708202956104422,
        pitch: 0.17453292519943295,
        distance: 80
      }),
      tolerance: 0.015
    }),
    clickFraction: Object.freeze({ x: 0.5, y: 0.35 }),
    destination: Object.freeze({ regionId: 0x6e4c, x: 437.106, y: 36.167, z: 526.986 }),
    destinationToleranceNative: 6,
    minimumMeasuredTravelNative: 25
  })
});

export function missionRegionGroundDistance(left, right) {
  if (!left || !right) return Number.POSITIVE_INFINITY;
  const leftRegion = Number(left.regionId) & 0xffff;
  const rightRegion = Number(right.regionId) & 0xffff;
  const dx =
    ((rightRegion & 0xff) - (leftRegion & 0xff)) * 1920 +
    Number(right.x) - Number(left.x);
  const dz =
    (((rightRegion >>> 8) & 0xff) - ((leftRegion >>> 8) & 0xff)) * 1920 +
    Number(right.z) - Number(left.z);
  return Math.hypot(dx, dz);
}

export function assertMissionFixturePosition(actual, fixture) {
  const distance = missionRegionGroundDistance(fixture.start, actual);
  const verticalDelta = Math.abs(Number(actual?.y) - fixture.start.y);
  const yawDelta = angularDistance(Number(actual?.yaw), fixture.startYawRadians);
  if (
    (Number(actual?.regionId) & 0xffff) !== fixture.start.regionId ||
    distance > fixture.startToleranceNative ||
    verticalDelta > fixture.startToleranceNative
  ) {
    throw new Error(
      `Mission fixture ${fixture.id} is not resident: expected ` +
        `${formatPosition(fixture.start)} +/-${fixture.startToleranceNative}u, got ` +
        `${formatPosition(actual)} (groundDelta=${formatNumber(distance)}u, ` +
        `verticalDelta=${formatNumber(verticalDelta)}u). Reset the fixture before boot.`
    );
  }
  if (actual?.moving === true) {
    throw new Error(`Mission fixture ${fixture.id} is still moving before measurement`);
  }
  if (yawDelta > fixture.startYawToleranceRadians) {
    throw new Error(
      `Mission fixture ${fixture.id} facing drifted by ${formatNumber(yawDelta)}rad; ` +
        "reset the two-leg fixture before boot"
    );
  }
  return { distance, verticalDelta, yawDelta };
}

export async function verifyMissionMovementFixture(page, fixture) {
  const camera = await applyMissionMovementFixtureCamera(page, fixture);
  const snapshot = await page.evaluate(({ clickFraction }) => {
    const canvas = document.querySelector('canvas[aria-label="Silkroad mission world"]');
    const probe = window.__sroGroundMoveProbe;
    const truth = window.__wipMissionBridge?.truth?.() ?? null;
    if (!(canvas instanceof HTMLCanvasElement)) {
      throw new Error("Mission world canvas is unavailable");
    }
    if (typeof probe !== "function") {
      throw new Error("__sroGroundMoveProbe is unavailable");
    }
    if (!truth) {
      throw new Error("Mission movement truth is unavailable");
    }
    const rect = canvas.getBoundingClientRect();
    const clientX = Math.round(rect.left + rect.width * clickFraction.x);
    const clientY = Math.round(rect.top + rect.height * clickFraction.y);
    const element = document.elementFromPoint(clientX, clientY);
    const interactiveSelector = element?.closest(
      "button,input,textarea,select,a,[role=button],[data-cif-window]"
    )?.tagName ?? null;
    const characterAtPoint = window.__sroCharacterPickProbe?.(clientX, clientY) ?? null;
    return {
      truth: {
        regionId: Number(truth.regionId) & 0xffff,
        x: Number(truth.x),
        y: Number(truth.y),
        z: Number(truth.z),
        yaw: Number(truth.yaw),
        moving: truth.moving === true
      },
      canvas: { x: rect.left, y: rect.top, width: rect.width, height: rect.height },
      clientX,
      clientY,
      interactiveSelector,
      characterAtPoint,
      ground: probe(clientX, clientY)
    };
  }, { clickFraction: fixture.clickFraction });

  const position = assertMissionFixturePosition(snapshot.truth, fixture);
  const expectedViewport = fixture.viewport;
  if (
    Math.abs(snapshot.canvas.width - expectedViewport.width) > 1 ||
    Math.abs(snapshot.canvas.height - expectedViewport.height) > 1
  ) {
    throw new Error(
      `Mission fixture ${fixture.id} requires a ${expectedViewport.width}x${expectedViewport.height} ` +
        `world canvas; got ${snapshot.canvas.width}x${snapshot.canvas.height}`
    );
  }
  if (snapshot.interactiveSelector || snapshot.characterAtPoint) {
    throw new Error(
      `Mission fixture ${fixture.id} click is obstructed: ` +
        `ui=${snapshot.interactiveSelector ?? "none"} actor=${snapshot.characterAtPoint ?? "none"}`
    );
  }
  if (snapshot.ground?.blockedBy !== null || !snapshot.ground?.destination) {
    throw new Error(
      `Mission fixture ${fixture.id} read-only native pick is not legal: ` +
        JSON.stringify(snapshot.ground ?? null)
    );
  }
  const destinationDistance = missionRegionGroundDistance(
    fixture.destination,
    snapshot.ground.destination
  );
  const destinationVerticalDelta = Math.abs(
    Number(snapshot.ground.destination.y) - fixture.destination.y
  );
  if (
    (Number(snapshot.ground.destination.regionId) & 0xffff) !== fixture.destination.regionId ||
    destinationDistance > fixture.destinationToleranceNative ||
    destinationVerticalDelta > fixture.destinationToleranceNative
  ) {
    throw new Error(
      `Mission fixture ${fixture.id} camera/pick contract drifted: expected ` +
        `${formatPosition(fixture.destination)}, got ${formatPosition(snapshot.ground.destination)}. ` +
        "Re-certify the route outside the benchmark; do not search during measurement."
    );
  }
  return {
    fixtureId: fixture.id,
    camera,
    position,
    truth: snapshot.truth,
    canvas: snapshot.canvas,
    clientX: snapshot.clientX,
    clientY: snapshot.clientY,
    destination: snapshot.ground.destination,
    destinationDistance,
    destinationVerticalDelta
  };
}

export async function applyMissionMovementFixtureCamera(page, fixture) {
  const cameraContract = fixture.camera;
  if (!cameraContract) {
    throw new Error(`Mission fixture ${fixture.id} has no certified camera contract`);
  }
  const before = await readMissionCamera(page);
  if (cameraMatches(before, cameraContract.measured, cameraContract.tolerance)) {
    return { setupApplied: false, before, after: before };
  }
  if (!cameraMatches(before, cameraContract.initial, cameraContract.tolerance)) {
    throw new Error(
      `Mission fixture ${fixture.id} camera is neither fresh nor already certified: ` +
        JSON.stringify(before)
    );
  }
  const canvas = page.locator('canvas[aria-label="Silkroad mission world"]');
  const bounds = await canvas.boundingBox();
  if (!bounds) throw new Error(`Mission fixture ${fixture.id} canvas has no camera bounds`);
  const setup = cameraContract.setup;
  if (!setup) {
    throw new Error(
      `Mission fixture ${fixture.id} camera drifted but has no certified setup transform`
    );
  }
  const startX = bounds.x + bounds.width * setup.startX;
  const startY = bounds.y + bounds.height * setup.startY;
  await page.mouse.move(startX, startY);
  await page.mouse.down({ button: "right" });
  try {
    await page.mouse.move(startX + bounds.width * setup.deltaX, startY, {
      steps: setup.steps
    });
  } finally {
    await page.mouse.up({ button: "right" }).catch(() => undefined);
  }
  await page.waitForTimeout(250);
  const after = await readMissionCamera(page);
  if (!cameraMatches(after, cameraContract.measured, cameraContract.tolerance)) {
    throw new Error(
      `Mission fixture ${fixture.id} camera setup drifted: expected ` +
        `${JSON.stringify(cameraContract.measured)}, got ${JSON.stringify(after)}`
    );
  }
  return { setupApplied: true, before, after };
}

async function readMissionCamera(page) {
  const camera = await page.evaluate(() => window.__missionCameraDebug ?? null);
  if (!camera) throw new Error("__missionCameraDebug is unavailable for fixture verification");
  return {
    yaw: Number(camera.yaw),
    pitch: Number(camera.pitch),
    distance: Number(camera.requestedDistance),
    effectiveDistance: Number(camera.effectiveDistance)
  };
}

function cameraMatches(actual, expected, tolerance) {
  const yawDelta = angularDistance(Number(actual?.yaw), expected.yaw);
  return (
    yawDelta <= tolerance &&
    Math.abs(Number(actual?.pitch) - expected.pitch) <= tolerance &&
    Math.abs(Number(actual?.distance) - expected.distance) <= 0.1
  );
}

function angularDistance(actual, expected) {
  if (!Number.isFinite(actual) || !Number.isFinite(expected)) return Number.POSITIVE_INFINITY;
  return Math.abs(Math.atan2(Math.sin(actual - expected), Math.cos(actual - expected)));
}

export async function dispatchSingleMissionMovement(page, prepared, fixture, options = {}) {
  const timeoutMs = Number(options.timeoutMs ?? 3000);
  const expectedDestination = prepared.destination;
  const baseline = await page.evaluate(() => ({
    transportSequence: Number(window.__sroGoTransportTelemetry?.nextSequence ?? 1),
    bridgeTraceCount: Number(window.__wipMissionBridge?.traces?.length ?? 0),
    bridgeDispatchCount: Number(window.__wipMissionBridge?.dispatched?.length ?? 0)
  }));

  // This is intentionally the only mutating input in the workload lifecycle.
  await page.mouse.click(prepared.clientX, prepared.clientY, { button: "left" });

  const deadline = Date.now() + timeoutMs;
  let evidence = null;
  while (Date.now() < deadline) {
    evidence = await readOneShotEvidence(page, baseline, expectedDestination);
    assertAtMostOneMovementCommand(evidence);
    if (hasOneShotAuthorityAnchor(evidence)) break;
    await page.waitForTimeout(Math.min(25, Math.max(1, deadline - Date.now())));
  }
  evidence ??= await readOneShotEvidence(page, baseline, expectedDestination);
  assertAtMostOneMovementCommand(evidence);
  if (!hasOneShotAuthorityAnchor(evidence)) {
    throw new Error(
      `Mission fixture ${fixture.id} one-shot command received no authoritative 0xB738; ` +
        `no retry is allowed: ` +
        JSON.stringify(evidence)
    );
  }
  return {
    input: "left-click",
    inputDispatches: 1,
    fixtureId: fixture.id,
    observationBaseline: baseline,
    expectedDestination,
    anchor: {
      event: "native-frame-received:0xB738",
      atUnixMs: evidence.localMoveAck.atUnixMs,
      sequence: evidence.localMoveAck.sequence
    },
    evidence
  };
}

function hasOneShotAuthorityAnchor(evidence) {
  return evidence?.submittedMoveCount === 1 && evidence?.localMoveAck != null;
}

export async function verifyMissionMovementConvergence(page, command, fixture) {
  const evidence = await readOneShotEvidence(
    page,
    command.observationBaseline,
    command.expectedDestination
  );
  assertAtMostOneMovementCommand(evidence);
  if (!hasConvergedOneShotMovement(evidence, command.expectedDestination)) {
    throw new Error(
      `Mission fixture ${fixture.id} did not converge after its captured window: ` +
        JSON.stringify(evidence)
    );
  }
  return evidence;
}

export function hasConvergedOneShotMovement(evidence, expectedDestination) {
  const arrived =
    evidence?.truth != null &&
    missionRegionGroundDistance(evidence.truth, expectedDestination) <= 3 &&
    Math.abs(Number(evidence.truth.y) - Number(expectedDestination?.y)) <= 3;
  return (
    evidence?.submittedMoveCount === 1 &&
    evidence?.localMoveAck != null &&
    evidence?.foldedExpectedMove === true &&
    (isAcceptedOneShotMovement(evidence) || arrived)
  );
}

export function assertAtMostOneMovementCommand(evidence) {
  if (Number(evidence?.submittedMoveCount) > 1) {
    throw new Error(
      `Benchmark workload emitted ${evidence.submittedMoveCount} native 0x7738 commands; expected at most one`
    );
  }
}

export function isAcceptedOneShotMovement(evidence) {
  return (
    evidence?.submittedMoveCount === 1 &&
    evidence?.localMoveAck != null &&
    evidence?.foldedExpectedMove === true &&
    evidence?.truth?.moving === true &&
    (evidence?.locomotion === "run" || evidence?.locomotion === "walk")
  );
}

async function readOneShotEvidence(page, baseline, expectedDestination) {
  return page.evaluate(({ start, expected }) => {
    const telemetry = window.__sroGoTransportTelemetry;
    const bridge = window.__wipMissionBridge;
    const events = (telemetry?.events ?? []).filter(
      (event) => Number(event.sequence) >= start.transportSequence
    );
    const submittedMoves = events.filter(
      (event) => event.type === "native-frame-submitted" && event.nativeOpcode === 0x7738
    );
    const localMoveAcks = events.filter(
      (event) =>
        event.type === "native-frame-received" &&
        event.nativeOpcode === 0xb738 &&
        event.payloadByteLength === 14
    );
    const traces = (bridge?.traces ?? []).slice(start.bridgeTraceCount);
    const expectedGoal =
      `goal latched {region=${expected.regionId} x=${Math.trunc(expected.x).toFixed(1)} ` +
      `z=${Math.trunc(expected.z).toFixed(1)}}`;
    return {
      submittedMoveCount: submittedMoves.length,
      submittedMoves,
      localMoveAck: localMoveAcks[0] ?? null,
      localMoveAckCount: localMoveAcks.length,
      foldedExpectedMove: traces.some(
        (line) => line.includes("0xb738 WIP shadow leg") && line.includes(expectedGoal)
      ),
      bridgeDispatches: (bridge?.dispatched ?? []).slice(start.bridgeDispatchCount),
      traces: traces.slice(-20),
      truth: bridge?.truth?.() ?? null,
      locomotion: window.__missionLocalLocomotion?.renderLocomotion ?? null
    };
  }, { start: baseline, expected: expectedDestination });
}

export async function resetMissionMovementFixture(options) {
  const fixture = options.fixture;
  const timeoutMs = Number(options.timeoutMs ?? 180_000);
  const deadline = Date.now() + timeoutMs;
  options.onLog?.(
    `resetting ${options.characterName} to ${fixture.id}: ${formatPosition(fixture.start)}`
  );
  const session = options.session ?? await openProbeAgentSession(options);
  const remainingMs = deadline - Date.now();
  if (remainingMs <= 0) {
    throw new Error(`Mission fixture ${fixture.id} reset exhausted its single ${timeoutMs}ms budget before reset authority`);
  }
  const angle = nativeHeadingWord(fixture.startYawRadians);
  const requestBody = JSON.stringify({
    characterName: options.characterName,
    fixtureId: fixture.id,
    movementMode: fixture.movementMode,
    spawn: { ...fixture.start, angle },
    // A load scenario's level, intellect and learned skills
    // (benchmark_fixture.go benchmarkFixtureLoadout).
    ...(fixture.loadout ? { loadout: fixture.loadout } : {})
  });
  let response;
  for (;;) {
    const requestRemainingMs = deadline - Date.now();
    if (requestRemainingMs <= 0) {
      throw new Error(`Mission fixture ${fixture.id} reset exhausted its single ${timeoutMs}ms budget waiting for character control`);
    }
    try {
      response = await fetchProbeSessionJson(session, BENCHMARK_FIXTURE_RESET_PATH, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: requestBody,
        signal: AbortSignal.timeout(requestRemainingMs)
      });
      break;
    } catch (error) {
      if (!(error instanceof ProbeSessionHttpError) || error.status !== 409 ||
          error.body?.code !== "CHARACTER_IN_PLAY") {
        throw error;
      }
      options.onLog?.(`fixture ${fixture.id} waiting for the previous live character lease to release`);
      await new Promise((resolve) => setTimeout(resolve, Math.min(1000, requestRemainingMs)));
    }
  }
  const persisted = await readProbeCharacterSpawnFromSession(session, options.characterName);
  assertFixtureResetReadback(persisted, fixture.start, angle, fixture.id);
  options.onLog?.(
    `fixture ${fixture.id} ${response.outcome ?? "reset"}: ` +
      `${formatPosition(persisted)} angle=0x${angle.toString(16).padStart(4, "0")}`
  );
  return { fixtureId: fixture.id, response, persisted };
}

/**
 * Ensure the approved disposable benchmark identity exists before any browser
 * boots. Provisioning uses the authenticated loopback character authority and
 * the ordinary native-shaped create contract; it never substitutes another
 * character when the requested scratch identity is unavailable.
 */
export async function ensureMissionBenchmarkFixtureCharacter(options) {
  const characterName = String(options.characterName ?? "").trim();
  if (characterName === "") {
    throw new Error("benchmark fixture character name is required");
  }
  assertCharacterAllowed(characterName, {
    context: "ensureMissionBenchmarkFixtureCharacter()"
  });
  const session = options.session ?? await openProbeAgentSession(options);
  const existing = await readProbeCharacterFromSession(session, characterName);
  if (existing) {
    options.onLog?.(`benchmark fixture identity ${characterName} already exists`);
    return { characterName, provisioned: false, character: existing };
  }

  options.onLog?.(`provisioning approved benchmark fixture identity ${characterName}`);
  const response = await fetchProbeSessionJson(
    session,
    BENCHMARK_FIXTURE_CHARACTER_CREATE_PATH,
    {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        divisionId: session.divisionId,
        characterName,
        modelCodename: "CHAR_CH_MAN_ADVENTURER",
        heightIndex: 0,
        volumeIndex: 0,
        weaponIndex: 1,
        protectorIndex: 0,
        armorSelected: false,
        weaponSelected: true
      })
    }
  );
  const character = await readProbeCharacterFromSession(session, characterName);
  if (response?.nativeResult !== 1 || !character) {
    throw new Error(
      `approved benchmark fixture ${characterName} could not be provisioned: ` +
        JSON.stringify(response)
    );
  }
  options.onLog?.(`provisioned approved benchmark fixture identity ${characterName}`);
  return { characterName, provisioned: true, response, character };
}

async function readProbeCharacterFromSession(session, characterName) {
  const body = await fetchProbeSessionJson(session, "/character/list");
  return Array.isArray(body?.characters)
    ? body.characters.find((entry) => entry?.name === characterName) ?? null
    : null;
}

function nativeHeadingWord(radians) {
  const turn = Math.PI * 2;
  const normalized = ((Number(radians) % turn) + turn) % turn;
  return Math.round((normalized / turn) * 0xffff) & 0xffff;
}

function assertFixtureResetReadback(actual, expected, expectedAngle, fixtureId) {
  const drift = missionRegionGroundDistance(actual, expected);
  if (!actual || drift > 0.001 || Math.abs(Number(actual.y) - Number(expected.y)) > 0.001 ||
      Number(actual.angle) !== expectedAngle) {
    throw new Error(
      `Mission fixture ${fixtureId} reset readback mismatch: expected ${formatPosition(expected)} ` +
        `angle=0x${expectedAngle.toString(16)}, got ${formatPosition(actual)} angle=${actual?.angle ?? "missing"}`
    );
  }
}

function formatPosition(value) {
  if (!value) return "<missing>";
  return (
    `0x${(Number(value.regionId) & 0xffff).toString(16)} ` +
    `(${formatNumber(value.x)},${formatNumber(value.y)},${formatNumber(value.z)})`
  );
}

function formatNumber(value) {
  return Number.isFinite(Number(value)) ? Number(value).toFixed(2) : "NaN";
}
