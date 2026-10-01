import type { InputBatch, InputCommand, SimulationInput } from "@/engine/contracts/input";
export function createSimulationInput(): SimulationInput {
    let received = 0, accepted = 0;
    const pending: InputCommand[] = [];
    const keys = new Set<string>();
    return {
        receive(batch: InputBatch) {
            if (!batch.commands.length || batch.commands.length > 512 || pending.length + batch.commands.length > 2048)
                throw new Error("Invalid simulation input batch size");
            if (batch.first !== received + 1 || batch.last !== batch.first + batch.commands.length - 1)
                throw new Error("Input sequence gap");
            for (let i = 0; i < batch.commands.length; i++) {
                const c = batch.commands[i]!;
                if (c.sequence !== batch.first + i || !Number.isFinite(c.timeMs))
                    throw new Error("Invalid input command");
                if (c.kind === "pointer" && (!Number.isFinite(c.x) || !Number.isFinite(c.y) || !Number.isInteger(c.buttons)))
                    throw new Error("Invalid pointer input");
                if (c.kind === "key" && (typeof c.code !== "string" || c.code.length > 64 || typeof c.down !== "boolean"))
                    throw new Error("Invalid key input");
                if (c.kind === "wheel" && !Number.isFinite(c.delta))
                    throw new Error("Invalid wheel input");
                if (!["pointer", "key", "wheel", "release"].includes(c.kind))
                    throw new Error("Unknown input kind");
            }
            pending.push(...batch.commands);
            received = batch.last;
        },
        commit() {
            for (const command of pending) {
                if (command.kind === "release") {
                    keys.clear();
                }
                else if (command.kind === "key") {
                    if (command.down)
                        keys.add(command.code);
                    else
                        keys.delete(command.code);
                }
                accepted = command.sequence;
            }
            pending.length = 0;
            return accepted;
        },
        lastAccepted: () => accepted
    };
}
