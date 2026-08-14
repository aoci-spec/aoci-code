import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type JournalAction = "read" | "write" | "administer";

const required: Record<JournalAction, readonly string[]> = {
  read: ["journal:read"],
  write: ["journal:read", "journal:write"],
  administer: ["journal:read", "journal:write", "journal:admin"],
};

/** Tenant-scoped authorization for journal batch operations. */
export function assertJournalAccess(tenantId: string, action: JournalAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("journal access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`journal ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayJournal(tenantId: string, action: JournalAction): boolean {
  try {
    assertJournalAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): JournalAction[] {
  const actions: JournalAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayJournal(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("journal belongs to a different tenant");
  }
}
