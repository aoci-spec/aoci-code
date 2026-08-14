import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type SettlementAction = "read" | "write" | "administer";

const required: Record<SettlementAction, readonly string[]> = {
  read: ["settlement:read"],
  write: ["settlement:read", "settlement:write"],
  administer: ["settlement:read", "settlement:write", "settlement:admin"],
};

/** Tenant-scoped authorization for settlement run operations. */
export function assertSettlementAccess(tenantId: string, action: SettlementAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("settlement access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`settlement ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function maySettlement(tenantId: string, action: SettlementAction): boolean {
  try {
    assertSettlementAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): SettlementAction[] {
  const actions: SettlementAction[] = ["read", "write", "administer"];
  return actions.filter((action) => maySettlement(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("settlement belongs to a different tenant");
  }
}
