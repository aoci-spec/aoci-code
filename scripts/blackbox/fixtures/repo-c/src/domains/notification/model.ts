import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one outbound notification. */
export interface Notification {
  readonly id: string;
  readonly tenantId: string;
  status: NotificationStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly NotificationChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface NotificationChange {
  readonly at: string;
  readonly from: NotificationStatus;
  readonly to: NotificationStatus;
}

export type NotificationStatus = "draft" | "active" | "settled" | "cancelled";

export const notificationStatuses: readonly NotificationStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a outbound notification; anything else is rejected upstream. */
const transitions: Record<NotificationStatus, readonly NotificationStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canNotificationTransition(from: NotificationStatus, to: NotificationStatus): boolean {
  return transitions[from].includes(to);
}

export function isNotificationTerminal(value: Notification): boolean {
  return transitions[value.status].length === 0;
}

export function newNotification(id: string, tenantId: string, reference: string): Notification {
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

export function touchNotification(value: Notification): Notification {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyNotificationTransition(value: Notification, to: NotificationStatus): Notification {
  const change: NotificationChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withNotificationAmount(value: Notification, amountCents: number): Notification {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("notification amount must be a non-negative integer");
  }
  return touchNotification({ ...value, amountCents });
}

export function withNotificationLabel(value: Notification, label: string): Notification {
  if (label.trim().length === 0) {
    throw new ValidationError("notification label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchNotification({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutNotificationLabel(value: Notification, label: string): Notification {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchNotification({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateNotification(value: Notification): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("notification requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("notification reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("notification amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("notification updatedAt precedes createdAt");
  }
}

export function compareNotification(left: Notification, right: Notification): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeNotification(value: Notification): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function notificationStatusCounts(values: readonly Notification[]): Record<NotificationStatus, number> {
  const counts: Record<NotificationStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
