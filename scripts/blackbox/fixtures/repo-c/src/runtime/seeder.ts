import { auditEvent } from "../infra/audit";
import { isoTimestamp } from "../infra/time";

export interface SeederOptions {
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}

const defaults: SeederOptions = { enabled: true, label: "seeder", budgetMillis: 425 };

/** Runtime seeder concern, wired once during boot and never per request. */
export function configureSeeder(overrides: Partial<SeederOptions> = {}): SeederOptions {
  const options = { ...defaults, ...overrides };
  if (options.budgetMillis <= 0) {
    throw new Error("seeder budget must be positive");
  }
  auditEvent("runtime.seeder.configured", { label: options.label, at: isoTimestamp() });
  return options;
}

export function describeSeeder(options: SeederOptions): string {
  return `${options.label}: ${options.enabled ? "on" : "off"} (${options.budgetMillis}ms)`;
}

export function withinSeederBudget(options: SeederOptions, elapsedMillis: number): boolean {
  return options.enabled && elapsedMillis <= options.budgetMillis;
}
