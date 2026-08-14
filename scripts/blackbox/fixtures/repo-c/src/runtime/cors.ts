import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface CorsOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: CorsOptions = { enabled: true, label: "cors", budgetMillis: 275 };

/** Runtime cors concern, wired once during boot and never per request. */
export function configureCors(overrides: Partial<CorsOptions> = {}): CorsOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("cors budget must be positive");
  }
  auditEvent("runtime.cors.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeCors(options: CorsOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinCorsBudget(options: CorsOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
