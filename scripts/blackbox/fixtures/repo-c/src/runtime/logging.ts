import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface LoggingOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: LoggingOptions = { enabled: true, label: "logging", budgetMillis: 100 };

/** Runtime logging concern, wired once during boot and never per request. */
export function configureLogging(overrides: Partial<LoggingOptions> = {}): LoggingOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("logging budget must be positive");
  }
  auditEvent("runtime.logging.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeLogging(options: LoggingOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinLoggingBudget(options: LoggingOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
