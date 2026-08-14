import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one journal batch. */
export interface Journal {
  readonly id: string;
  readonly tenantId: string;
  status: JournalStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly JournalChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface JournalChange {
  readonly at: string;
  readonly from: JournalStatus;
  readonly to: JournalStatus;
}

export type JournalStatus = "draft" | "active" | "settled" | "cancelled";

export const journalStatuses: readonly JournalStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a journal batch; anything else is rejected upstream. */
const transitions: Record<JournalStatus, readonly JournalStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canJournalTransition(from: JournalStatus, to: JournalStatus): boolean {
  return transitions[from].includes(to);
}

export function isJournalTerminal(value: Journal): boolean {
  return transitions[value.status].length === 0;
}

export function newJournal(id: string, tenantId: string, reference: string): Journal {
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

export function touchJournal(value: Journal): Journal {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyJournalTransition(value: Journal, to: JournalStatus): Journal {
  const change: JournalChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withJournalAmount(value: Journal, amountCents: number): Journal {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("journal amount must be a non-negative integer");
  }
  return touchJournal({ ...value, amountCents });
}

export function withJournalLabel(value: Journal, label: string): Journal {
  if (label.trim().length === 0) {
    throw new ValidationError("journal label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchJournal({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutJournalLabel(value: Journal, label: string): Journal {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchJournal({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateJournal(value: Journal): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("journal requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("journal reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("journal amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("journal updatedAt precedes createdAt");
  }
}

export function compareJournal(left: Journal, right: Journal): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeJournal(value: Journal): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function journalStatusCounts(values: readonly Journal[]): Record<JournalStatus, number> {
  const counts: Record<JournalStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
