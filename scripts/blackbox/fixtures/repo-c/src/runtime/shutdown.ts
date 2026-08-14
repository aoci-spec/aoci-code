import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface ShutdownOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: ShutdownOptions = { enabled: true, label: "shutdown", budgetMillis: 200 };

/** Runtime shutdown concern, wired once during boot and never per request. */
export function configureShutdown(overrides: Partial<ShutdownOptions> = {}): ShutdownOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("shutdown budget must be positive");
  }
  auditEvent("runtime.shutdown.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeShutdown(options: ShutdownOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinShutdownBudget(options: ShutdownOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
