import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface SerializationOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: SerializationOptions = { enabled: true, label: "serialization", budgetMillis: 475 };

/** Runtime serialization concern, wired once during boot and never per request. */
export function configureSerialization(overrides: Partial<SerializationOptions> = {}): SerializationOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("serialization budget must be positive");
  }
  auditEvent("runtime.serialization.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeSerialization(options: SerializationOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinSerializationBudget(options: SerializationOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
