import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface RatelimitOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: RatelimitOptions = { enabled: true, label: "ratelimit", budgetMillis: 250 };

/** Runtime ratelimit concern, wired once during boot and never per request. */
export function configureRatelimit(overrides: Partial<RatelimitOptions> = {}): RatelimitOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("ratelimit budget must be positive");
  }
  auditEvent("runtime.ratelimit.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeRatelimit(options: RatelimitOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinRatelimitBudget(options: RatelimitOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
