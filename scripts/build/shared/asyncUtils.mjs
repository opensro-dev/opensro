/**
 * Run `worker` over `items` with at most `limit` jobs in flight.
 * Results retain input order even when workers finish out of order.
 */
export async function mapWithConcurrency(items, limit, worker) {
  const results = new Array(items.length);
  let nextIndex = 0;
  const laneCount = Math.max(1, Math.min(normalizeConcurrency(limit), items.length));
  const lanes = Array.from({ length: laneCount }, async () => {
    for (;;) {
      const index = nextIndex;
      nextIndex += 1;
      if (index >= items.length) return;
      results[index] = await worker(items[index], index);
    }
  });
  await Promise.all(lanes);
  return results;
}

/**
 * A shared bound for work started from several places at once: run(task)
 * starts task when fewer than `limit` tasks are running, else queues it.
 * Callers spread over many groups share one budget instead of one each.
 */
export function createLimiter(limit) {
  const capacity = normalizeConcurrency(limit);
  const waiting = [];
  let running = 0;
  const next = () => {
    if (running >= capacity || waiting.length === 0) return;
    running += 1;
    const { task, resolve, reject } = waiting.shift();
    Promise.resolve()
      .then(task)
      .then(resolve, reject)
      .finally(() => {
        running -= 1;
        next();
      });
  };
  return (task) =>
    new Promise((resolve, reject) => {
      waiting.push({ task, resolve, reject });
      next();
    });
}

function normalizeConcurrency(value) {
  return Number.isFinite(value) ? Math.max(1, Math.floor(value)) : 1;
}
