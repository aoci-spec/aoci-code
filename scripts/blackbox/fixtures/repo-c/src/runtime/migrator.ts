import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface MigratorOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: MigratorOptions = { enabled: true, label: "migrator", budgetMillis: 400 };

/** Runtime migrator concern, wired once during boot and never per request. */
export function configureMigrator(overrides: Partial<MigratorOptions> = {}): MigratorOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("migrator budget must be positive");
  }
  auditEvent("runtime.migrator.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeMigrator(options: MigratorOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinMigratorBudget(options: MigratorOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
