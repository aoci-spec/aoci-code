import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one tracking event. */
export interface Tracking {
  readonly id: string;
  readonly tenantId: string;
  status: TrackingStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly TrackingChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface TrackingChange {
  readonly at: string;
  readonly from: TrackingStatus;
  readonly to: TrackingStatus;
}

export type TrackingStatus = "draft" | "active" | "settled" | "cancelled";

export const trackingStatuses: readonly TrackingStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a tracking event; anything else is rejected upstream. */
const transitions: Record<TrackingStatus, readonly TrackingStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canTrackingTransition(from: TrackingStatus, to: TrackingStatus): boolean {
  return transitions[from].includes(to);
}

export function isTrackingTerminal(value: Tracking): boolean {
  return transitions[value.status].length === 0;
}

export function newTracking(id: string, tenantId: string, reference: string): Tracking {
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

export function touchTracking(value: Tracking): Tracking {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyTrackingTransition(value: Tracking, to: TrackingStatus): Tracking {
  const change: TrackingChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withTrackingAmount(value: Tracking, amountCents: number): Tracking {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("tracking amount must be a non-negative integer");
  }
  return touchTracking({ ...value, amountCents });
}

export function withTrackingLabel(value: Tracking, label: string): Tracking {
  if (label.trim().length === 0) {
    throw new ValidationError("tracking label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchTracking({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutTrackingLabel(value: Tracking, label: string): Tracking {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchTracking({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateTracking(value: Tracking): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("tracking requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("tracking reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("tracking amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("tracking updatedAt precedes createdAt");
  }
}

export function compareTracking(left: Tracking, right: Tracking): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeTracking(value: Tracking): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function trackingStatusCounts(values: readonly Tracking[]): Record<TrackingStatus, number> {
  const counts: Record<TrackingStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
