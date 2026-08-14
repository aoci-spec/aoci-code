import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface TracingOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: TracingOptions = { enabled: true, label: "tracing", budgetMillis: 225 };

/** Runtime tracing concern, wired once during boot and never per request. */
export function configureTracing(overrides: Partial<TracingOptions> = {}): TracingOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("tracing budget must be positive");
  }
  auditEvent("runtime.tracing.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeTracing(options: TracingOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinTracingBudget(options: TracingOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
