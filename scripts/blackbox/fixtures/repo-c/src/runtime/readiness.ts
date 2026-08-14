import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface ReadinessOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: ReadinessOptions = { enabled: true, label: "readiness", budgetMillis: 175 };

/** Runtime readiness concern, wired once during boot and never per request. */
export function configureReadiness(overrides: Partial<ReadinessOptions> = {}): ReadinessOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("readiness budget must be positive");
  }
  auditEvent("runtime.readiness.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeReadiness(options: ReadinessOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinReadinessBudget(options: ReadinessOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
