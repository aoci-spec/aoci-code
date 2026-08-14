import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface CacheOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: CacheOptions = { enabled: true, label: "cache", budgetMillis: 325 };

/** Runtime cache concern, wired once during boot and never per request. */
export function configureCache(overrides: Partial<CacheOptions> = {}): CacheOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("cache budget must be positive");
  }
  auditEvent("runtime.cache.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeCache(options: CacheOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinCacheBudget(options: CacheOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
