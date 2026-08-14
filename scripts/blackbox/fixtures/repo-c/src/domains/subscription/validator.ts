import { SubscriptionStatus, subscriptionStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface SubscriptionCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface SubscriptionTransitionInput {
  status: SubscriptionStatus;
}

export interface SubscriptionLabelInput {
  label: string;
}

export interface SubscriptionPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`subscription ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`subscription.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`subscription.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for recurring subscription writes; never trusts client types. */
export function parseSubscriptionCreate(body: unknown): SubscriptionCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("subscription.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseSubscriptionTransition(body: unknown): SubscriptionTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !subscriptionStatuses.includes(status as SubscriptionStatus)) {
    throw new ValidationError(`subscription.status must be one of ${subscriptionStatuses.join(", ")}`);
  }
  return { status: status as SubscriptionStatus };
}

export function parseSubscriptionLabel(body: unknown): SubscriptionLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseSubscriptionPage(query: unknown): SubscriptionPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("subscription.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("subscription.limit must be between 1 and 200");
  }
  return { offset, limit };
}
