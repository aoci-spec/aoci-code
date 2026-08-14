import { InventoryStatus, inventoryStatuses } from "./model";
import { ValidationError } from "../../infra/errors";

export interface InventoryCreateInput {
  id: string;
  reference: string;
  amountCents: number;
}

export interface InventoryTransitionInput {
  status: InventoryStatus;
}

export interface InventoryLabelInput {
  label: string;
}

export interface InventoryPageInput {
  offset: number;
  limit: number;
}

function requireRecord(body: unknown, what: string): Record<string, unknown> {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new ValidationError(`inventory ${what} body must be an object`);
  }
  return body as Record<string, unknown>;
}

function requireText(value: unknown, field: string, max = 190): string {
  if (typeof value !== "string" || value.trim().length === 0) {
    throw new ValidationError(`inventory.${field} must be a non-empty string`);
  }
  if (value.length > max) {
    throw new ValidationError(`inventory.${field} must be at most ${max} characters`);
  }
  return value;
}

/** Request-shape validation for stock position writes; never trusts client types. */
export function parseInventoryCreate(body: unknown): InventoryCreateInput {
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {
    throw new ValidationError("inventory.amountCents must be a non-negative integer");
  }
  return { id, reference, amountCents };
}

export function parseInventoryTransition(body: unknown): InventoryTransitionInput {
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !inventoryStatuses.includes(status as InventoryStatus)) {
    throw new ValidationError(`inventory.status must be one of ${inventoryStatuses.join(", ")}`);
  }
  return { status: status as InventoryStatus };
}

export function parseInventoryLabel(body: unknown): InventoryLabelInput {
  const record = requireRecord(body, "label");
  return { label: requireText(record.label, "label", 40) };
}

export function parseInventoryPage(query: unknown): InventoryPageInput {
  const record = requireRecord(query ?? {}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {
    throw new ValidationError("inventory.offset must be a non-negative integer");
  }
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {
    throw new ValidationError("inventory.limit must be between 1 and 200");
  }
  return { offset, limit };
}
