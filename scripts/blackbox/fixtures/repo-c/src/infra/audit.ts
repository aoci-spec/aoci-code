import { isoTimestamp } from "./time";

export interface AuditRecord {
  readonly at: string;
  readonly action: string;
  readonly detail: Record<string, unknown>;
}

const records: AuditRecord[] = [];

/** Append-only audit trail; callers never mutate the returned view. */
export function auditEvent(action: string, detail: Record<string, unknown>): void {
  records.push({ at: isoTimestamp(), action, detail });
}

export function auditTrail(): readonly AuditRecord[] {
  return records;
}
