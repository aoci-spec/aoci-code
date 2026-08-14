import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface QueueOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: QueueOptions = { enabled: true, label: "queue", budgetMillis: 350 };

/** Runtime queue concern, wired once during boot and never per request. */
export function configureQueue(overrides: Partial<QueueOptions> = {}): QueueOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("queue budget must be positive");
  }
  auditEvent("runtime.queue.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeQueue(options: QueueOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinQueueBudget(options: QueueOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
