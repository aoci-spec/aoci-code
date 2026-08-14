import { NotificationStatus, notificationStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface NotificationCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface NotificationTransitionInput {
  status: NotificationStatus;
}

export interface NotificationLabelInput {
  label: string;
}

export interface NotificationPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`notification ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`notification.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`notification.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for outbound notification writes; never trusts client types. */
export function parseNotificationCreate(body: unknown): NotificationCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("notification.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseNotificationTransition(body: unknown): NotificationTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !notificationStatuses.includes(status as NotificationStatus)) {
    throw new ValidationError(`notification.status must be one of ${notificationStatuses.join(", ")}`);
  }
  return { status: status as NotificationStatus };
}

export function parseNotificationLabel(body: unknown): NotificationLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseNotificationPage(query: unknown): NotificationPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("notification.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("notification.limit must be between 1 and 200");
  }
  return { offset, limit };
}
