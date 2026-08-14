import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type LedgerAction = "read" | "write" | "administer";

const required: Record<LedgerAction, readonly string[]> = {
  read: ["ledger:read"],
  write: ["ledger:read", "ledger:write"],
  administer: ["ledger:read", "ledger:write", "ledger:admin"],
};

/** Tenant-scoped authorization for accounting ledger entry operations. */
export function assertLedgerAccess(tenantId: string, action: LedgerAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("ledger access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`ledger ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayLedger(tenantId: string, action: LedgerAction): boolean {
  try {
    assertLedgerAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): LedgerAction[] {
  const actions: LedgerAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayLedger(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("ledger belongs to a different tenant");
  }
}
