import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface SortingOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: SortingOptions = { enabled: true, label: "sorting", budgetMillis: 625 };

/** Runtime sorting concern, wired once during boot and never per request. */
export function configureSorting(overrides: Partial<SortingOptions> = {}): SortingOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("sorting budget must be positive");
  }
  auditEvent("runtime.sorting.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeSorting(options: SortingOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinSortingBudget(options: SortingOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
