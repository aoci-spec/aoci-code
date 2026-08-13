/**
 * Time helpers shared across the service layer. All persisted timestamps are
 * ISO-8601 strings in UTC; durations are measured on the monotonic clock so
 * they are immune to wall-clock adjustments.
 */

export function nowIso(): string {
  return new Date().toISOString();
}

export function isoFromMs(epochMs: number): string {
  if (!Number.isFinite(epochMs) || epochMs < 0) {
    throw new RangeError(`epochMs must be a non-negative finite number, got ${epochMs}`);
  }
  return new Date(epochMs).toISOString();
}

/**
 * Starts a monotonic stopwatch. The returned function yields the elapsed time
 * in fractional milliseconds each time it is called.
 */
export function startTimer(): () => number {
  const startedAt = process.hrtime.bigint();
  return () => Number(process.hrtime.bigint() - startedAt) / 1e6;
}

/** True when `value` is a canonical ISO-8601 UTC timestamp (round-trips exactly). */
export function isIsoTimestamp(value: string): boolean {
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) && new Date(parsed).toISOString() === value;
}
