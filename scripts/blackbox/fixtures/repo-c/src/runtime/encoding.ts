import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface EncodingOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: EncodingOptions = { enabled: true, label: "encoding", budgetMillis: 575 };

/** Runtime encoding concern, wired once during boot and never per request. */
export function configureEncoding(overrides: Partial<EncodingOptions> = {}): EncodingOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("encoding budget must be positive");
  }
  auditEvent("runtime.encoding.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeEncoding(options: EncodingOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinEncodingBudget(options: EncodingOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
