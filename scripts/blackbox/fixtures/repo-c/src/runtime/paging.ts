import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface PagingOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: PagingOptions = { enabled: true, label: "paging", budgetMillis: 600 };

/** Runtime paging concern, wired once during boot and never per request. */
export function configurePaging(overrides: Partial<PagingOptions> = {}): PagingOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("paging budget must be positive");
  }
  auditEvent("runtime.paging.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describePaging(options: PagingOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinPagingBudget(options: PagingOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
