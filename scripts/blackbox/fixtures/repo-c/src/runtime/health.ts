import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface HealthOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: HealthOptions = { enabled: true, label: "health", budgetMillis: 150 };

/** Runtime health concern, wired once during boot and never per request. */
export function configureHealth(overrides: Partial<HealthOptions> = {}): HealthOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("health budget must be positive");
  }
  auditEvent("runtime.health.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeHealth(options: HealthOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinHealthBudget(options: HealthOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
