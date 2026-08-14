import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one tenant setting. */
export interface Setting {
  readonly id: string;
  readonly tenantId: string;
  status: SettingStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly SettingChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface SettingChange {
  readonly at: string;
  readonly from: SettingStatus;
  readonly to: SettingStatus;
}

export type SettingStatus = "draft" | "active" | "settled" | "cancelled";

export const settingStatuses: readonly SettingStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a tenant setting; anything else is rejected upstream. */
const transitions: Record<SettingStatus, readonly SettingStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canSettingTransition(from: SettingStatus, to: SettingStatus): boolean {
  return transitions[from].includes(to);
}

export function isSettingTerminal(value: Setting): boolean {
  return transitions[value.status].length === 0;
}

export function newSetting(id: string, tenantId: string, reference: string): Setting {
  const now = isoTimestamp();
  return {
    id,
    tenantId,
    status: "draft",
    amountCents: 0,
    reference,
    labels: [],
    history: [],
    createdAt: now,
    updatedAt: now,
  };
}

export function touchSetting(value: Setting): Setting {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applySettingTransition(value: Setting, to: SettingStatus): Setting {
  const change: SettingChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withSettingAmount(value: Setting, amountCents: number): Setting {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("setting amount must be a non-negative integer");
  }
  return touchSetting({ ...value, amountCents });
}

export function withSettingLabel(value: Setting, label: string): Setting {
  if (label.trim().length === 0) {
    throw new ValidationError("setting label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchSetting({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutSettingLabel(value: Setting, label: string): Setting {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchSetting({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateSetting(value: Setting): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("setting requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("setting reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("setting amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("setting updatedAt precedes createdAt");
  }
}

export function compareSetting(left: Setting, right: Setting): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeSetting(value: Setting): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function settingStatusCounts(values: readonly Setting[]): Record<SettingStatus, number> {
  const counts: Record<SettingStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
