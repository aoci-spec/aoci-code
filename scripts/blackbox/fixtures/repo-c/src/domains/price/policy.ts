import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type PriceAction = "read" | "write" | "administer";

const required: Record<PriceAction, readonly string[]> = {
  read: ["price:read"],
  write: ["price:read", "price:write"],
  administer: ["price:read", "price:write", "price:admin"],
};

/** Tenant-scoped authorization for price definition operations. */
export function assertPriceAccess(tenantId: string, action: PriceAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("price access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`price ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayPrice(tenantId: string, action: PriceAction): boolean {
  try {
    assertPriceAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): PriceAction[] {
  const actions: PriceAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayPrice(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("price belongs to a different tenant");
  }
}
