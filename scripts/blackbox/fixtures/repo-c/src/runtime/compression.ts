import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface CompressionOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: CompressionOptions = { enabled: true, label: "compression", budgetMillis: 300 };

/** Runtime compression concern, wired once during boot and never per request. */
export function configureCompression(overrides: Partial<CompressionOptions> = {}): CompressionOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("compression budget must be positive");
  }
  auditEvent("runtime.compression.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeCompression(options: CompressionOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinCompressionBudget(options: CompressionOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
