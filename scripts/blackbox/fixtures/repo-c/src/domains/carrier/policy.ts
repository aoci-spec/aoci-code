import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type CarrierAction = "read" | "write" | "administer";

const required: Record<CarrierAction, readonly string[]> = {
  read: ["carrier:read"],
  write: ["carrier:read", "carrier:write"],
  administer: ["carrier:read", "carrier:write", "carrier:admin"],
};

/** Tenant-scoped authorization for delivery carrier operations. */
export function assertCarrierAccess(tenantId: string, action: CarrierAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("carrier access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`carrier ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayCarrier(tenantId: string, action: CarrierAction): boolean {
  try {
    assertCarrierAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): CarrierAction[] {
  const actions: CarrierAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayCarrier(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("carrier belongs to a different tenant");
  }
}
