import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface RouterOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: RouterOptions = { enabled: true, label: "router", budgetMillis: 50 };

/** Runtime router concern, wired once during boot and never per request. */
export function configureRouter(overrides: Partial<RouterOptions> = {}): RouterOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("router budget must be positive");
  }
  auditEvent("runtime.router.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeRouter(options: RouterOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinRouterBudget(options: RouterOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
