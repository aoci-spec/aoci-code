import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface FeatureflagsOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: FeatureflagsOptions = { enabled: true, label: "featureflags", budgetMillis: 450 };

/** Runtime featureflags concern, wired once during boot and never per request. */
export function configureFeatureflags(overrides: Partial<FeatureflagsOptions> = {}): FeatureflagsOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("featureflags budget must be positive");
  }
  auditEvent("runtime.featureflags.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeFeatureflags(options: FeatureflagsOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinFeatureflagsBudget(options: FeatureflagsOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
