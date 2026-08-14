import { MessageStatus, messageStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface MessageCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface MessageTransitionInput {
  status: MessageStatus;
}

export interface MessageLabelInput {
  label: string;
}

export interface MessagePageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`message ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`message.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`message.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for customer message writes; never trusts client types. */
export function parseMessageCreate(body: unknown): MessageCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("message.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseMessageTransition(body: unknown): MessageTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !messageStatuses.includes(status as MessageStatus)) {
    throw new ValidationError(`message.status must be one of ${messageStatuses.join(", ")}`);
  }
  return { status: status as MessageStatus };
}

export function parseMessageLabel(body: unknown): MessageLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseMessagePage(query: unknown): MessagePageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("message.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("message.limit must be between 1 and 200");
  }
  return { offset, limit };
}
