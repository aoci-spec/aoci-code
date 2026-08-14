import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface HashingOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: HashingOptions = { enabled: true, label: "hashing", budgetMillis: 550 };

/** Runtime hashing concern, wired once during boot and never per request. */
export function configureHashing(overrides: Partial<HashingOptions> = {}): HashingOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("hashing budget must be positive");
  }
  auditEvent("runtime.hashing.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeHashing(options: HashingOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinHashingBudget(options: HashingOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
