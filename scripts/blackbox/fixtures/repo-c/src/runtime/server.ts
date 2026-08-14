import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface ServerOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: ServerOptions = { enabled: true, label: "server", budgetMillis: 25 };

/** Runtime server concern, wired once during boot and never per request. */
export function configureServer(overrides: Partial<ServerOptions> = {}): ServerOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("server budget must be positive");
  }
  auditEvent("runtime.server.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeServer(options: ServerOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinServerBudget(options: ServerOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
