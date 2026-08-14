import { TicketStatus, ticketStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface TicketCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface TicketTransitionInput {
  status: TicketStatus;
}

export interface TicketLabelInput {
  label: string;
}

export interface TicketPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`ticket ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`ticket.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`ticket.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for support ticket writes; never trusts client types. */
export function parseTicketCreate(body: unknown): TicketCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("ticket.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseTicketTransition(body: unknown): TicketTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !ticketStatuses.includes(status as TicketStatus)) {
    throw new ValidationError(`ticket.status must be one of ${ticketStatuses.join(", ")}`);
  }
  return { status: status as TicketStatus };
}

export function parseTicketLabel(body: unknown): TicketLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseTicketPage(query: unknown): TicketPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("ticket.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("ticket.limit must be between 1 and 200");
  }
  return { offset, limit };
}
