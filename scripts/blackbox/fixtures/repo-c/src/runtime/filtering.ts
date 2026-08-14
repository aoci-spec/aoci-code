import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface FilteringOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: FilteringOptions = { enabled: true, label: "filtering", budgetMillis: 650 };

/** Runtime filtering concern, wired once during boot and never per request. */
export function configureFiltering(overrides: Partial<FilteringOptions> = {}): FilteringOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("filtering budget must be positive");
  }
  auditEvent("runtime.filtering.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeFiltering(options: FilteringOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinFilteringBudget(options: FilteringOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
