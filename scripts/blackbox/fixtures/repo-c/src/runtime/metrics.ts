import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface MetricsOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: MetricsOptions = { enabled: true, label: "metrics", budgetMillis: 125 };

/** Runtime metrics concern, wired once during boot and never per request. */
export function configureMetrics(overrides: Partial<MetricsOptions> = {}): MetricsOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("metrics budget must be positive");
  }
  auditEvent("runtime.metrics.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeMetrics(options: MetricsOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinMetricsBudget(options: MetricsOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
