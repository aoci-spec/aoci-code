import { isoTimestamp, isBefore } from "../../infra/time";
import { ValidationError } from "../../infra/errors";

/** Durable shape of one support ticket. */
export interface Ticket {
  readonly id: string;
  readonly tenantId: string;
  status: TicketStatus;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly TicketChange[];
  readonly createdAt: string;
  updatedAt: string;
}

export interface TicketChange {
  readonly at: string;
  readonly from: TicketStatus;
  readonly to: TicketStatus;
}

export type TicketStatus = "draft" | "active" | "settled" | "cancelled";

export const ticketStatuses: readonly TicketStatus[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a support ticket; anything else is rejected upstream. */
const transitions: Record<TicketStatus, readonly TicketStatus[]> = {
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
};

export function canTicketTransition(from: TicketStatus, to: TicketStatus): boolean {
  return transitions[from].includes(to);
}

export function isTicketTerminal(value: Ticket): boolean {
  return transitions[value.status].length === 0;
}

export function newTicket(id: string, tenantId: string, reference: string): Ticket {
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

export function touchTicket(value: Ticket): Ticket {
  return { ...value, updatedAt: isoTimestamp() };
}

/** Applies a transition and records it; callers must check legality first. */
export function applyTicketTransition(value: Ticket, to: TicketStatus): Ticket {
  const change: TicketChange = { at: isoTimestamp(), from: value.status, to };
  return {
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  };
}

export function withTicketAmount(value: Ticket, amountCents: number): Ticket {
  if (!Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("ticket amount must be a non-negative integer");
  }
  return touchTicket({ ...value, amountCents });
}

export function withTicketLabel(value: Ticket, label: string): Ticket {
  if (label.trim().length === 0) {
    throw new ValidationError("ticket label must not be blank");
  }
  if (value.labels.includes(label)) {
    return value;
  }
  return touchTicket({ ...value, labels: [...value.labels, label].sort() });
}

export function withoutTicketLabel(value: Ticket, label: string): Ticket {
  if (!value.labels.includes(label)) {
    return value;
  }
  return touchTicket({ ...value, labels: value.labels.filter((item) => item !== label) });
}

export function validateTicket(value: Ticket): void {
  if (value.id.length === 0 || value.tenantId.length === 0) {
    throw new ValidationError("ticket requires both id and tenantId");
  }
  if (value.reference.trim().length === 0) {
    throw new ValidationError("ticket reference must not be blank");
  }
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {
    throw new ValidationError("ticket amount must be a non-negative integer");
  }
  if (isBefore(value.updatedAt, value.createdAt)) {
    throw new ValidationError("ticket updatedAt precedes createdAt");
  }
}

export function compareTicket(left: Ticket, right: Ticket): number {
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}

export function summarizeTicket(value: Ticket): string {
  return `${value.reference} (${value.status}, ${(value.amountCents / 100).toFixed(2)})`;
}

export function ticketStatusCounts(values: readonly Ticket[]): Record<TicketStatus, number> {
  const counts: Record<TicketStatus, number> = { draft: 0, active: 0, settled: 0, cancelled: 0 };
  for (const value of values) {
    counts[value.status] += 1;
  }
  return counts;
}
