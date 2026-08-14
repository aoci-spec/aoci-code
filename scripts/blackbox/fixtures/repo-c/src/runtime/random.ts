import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface RandomOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: RandomOptions = { enabled: true, label: "random", budgetMillis: 525 };

/** Runtime random concern, wired once during boot and never per request. */
export function configureRandom(overrides: Partial<RandomOptions> = {}): RandomOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("random budget must be positive");
  }
  auditEvent("runtime.random.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeRandom(options: RandomOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinRandomBudget(options: RandomOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
