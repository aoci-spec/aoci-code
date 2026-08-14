import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface SchedulerOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: SchedulerOptions = { enabled: true, label: "scheduler", budgetMillis: 375 };

/** Runtime scheduler concern, wired once during boot and never per request. */
export function configureScheduler(overrides: Partial<SchedulerOptions> = {}): SchedulerOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("scheduler budget must be positive");
  }
  auditEvent("runtime.scheduler.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeScheduler(options: SchedulerOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinSchedulerBudget(options: SchedulerOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
