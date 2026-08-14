import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one customer message. */
export interface Message {
  readonly id: string;
  readonly tenantId: string;
  status: MessageStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly MessageChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface MessageChange {
  readonly at: string;
  readonly from: MessageStatus;
  readonly to: MessageStatus;
}

export type MessageStatus = "draft" | "active" | "settled" | "cancelled";

export const messageStatuses: readonly MessageStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a customer message; anything else is rejected upstream. */
const transitions: Record<MessageStatus, readonly MessageStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canMessageTransition(from: MessageStatus, to: MessageStatus): boolean {
  return transitions[from].includes(to);
}

export function isMessageTerminal(value: Message): boolean {
  return transitions[value.status].length === 0;
}

export function newMessage(id: string, tenantId: string, reference: string): Message {
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

export function touchMessage(value: Message): Message {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyMessageTransition(value: Message, to: MessageStatus): Message {
  const change: MessageChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withMessageAmount(value: Message, amountCents: number): Message {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("message amount must be a non-negative integer");
  }
  return touchMessage({ ...value, amountCents });
}

export function withMessageLabel(value: Message, label: string): Message {
  if (label.trim().length === 0) {
    throw new ValidationError("message label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchMessage({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutMessageLabel(value: Message, label: string): Message {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchMessage({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateMessage(value: Message): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("message requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("message reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("message amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("message updatedAt precedes createdAt");
  }
}

export function compareMessage(left: Message, right: Message): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeMessage(value: Message): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function messageStatusCounts(values: readonly Message[]): Record<MessageStatus, number> {
  const counts: Record<MessageStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
