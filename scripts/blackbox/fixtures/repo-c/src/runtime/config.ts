import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface ConfigOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: ConfigOptions = { enabled: true, label: "config", budgetMillis: 75 };

/** Runtime config concern, wired once during boot and never per request. */
export function configureConfig(overrides: Partial<ConfigOptions> = {}): ConfigOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("config budget must be positive");
  }
  auditEvent("runtime.config.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeConfig(options: ConfigOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinConfigBudget(options: ConfigOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
