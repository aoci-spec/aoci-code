import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface ClockOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: ClockOptions = { enabled: true, label: "clock", budgetMillis: 500 };

/** Runtime clock concern, wired once during boot and never per request. */
export function configureClock(overrides: Partial<ClockOptions> = {}): ClockOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("clock budget must be positive");
  }
  auditEvent("runtime.clock.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeClock(options: ClockOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinClockBudget(options: ClockOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
