import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one subscription plan. */
export interface Plan {
  readonly id: string;
  readonly tenantId: string;
  status: PlanStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly PlanChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface PlanChange {
  readonly at: string;
  readonly from: PlanStatus;
  readonly to: PlanStatus;
}

export type PlanStatus = "draft" | "active" | "settled" | "cancelled";

export const planStatuses: readonly PlanStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a subscription plan; anything else is rejected upstream. */
const transitions: Record<PlanStatus, readonly PlanStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canPlanTransition(from: PlanStatus, to: PlanStatus): boolean {
  return transitions[from].includes(to);
}

export function isPlanTerminal(value: Plan): boolean {
  return transitions[value.status].length === 0;
}

export function newPlan(id: string, tenantId: string, reference: string): Plan {
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

export function touchPlan(value: Plan): Plan {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyPlanTransition(value: Plan, to: PlanStatus): Plan {
  const change: PlanChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withPlanAmount(value: Plan, amountCents: number): Plan {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("plan amount must be a non-negative integer");
  }
  return touchPlan({ ...value, amountCents });
}

export function withPlanLabel(value: Plan, label: string): Plan {
  if (label.trim().length === 0) {
    throw new ValidationError("plan label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchPlan({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutPlanLabel(value: Plan, label: string): Plan {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchPlan({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validatePlan(value: Plan): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("plan requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("plan reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("plan amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("plan updatedAt precedes createdAt");
  }
}

export function comparePlan(left: Plan, right: Plan): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizePlan(value: Plan): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function planStatusCounts(values: readonly Plan[]): Record<PlanStatus, number> {
  const counts: Record<PlanStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
