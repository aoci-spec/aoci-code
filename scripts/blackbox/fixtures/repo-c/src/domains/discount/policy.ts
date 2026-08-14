import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type DiscountAction = "read" | "write" | "administer";

const required: Record<DiscountAction, readonly string[]> = {
  read: ["discount:read"],
  write: ["discount:read", "discount:write"],
  administer: ["discount:read", "discount:write", "discount:admin"],
};

/** Tenant-scoped authorization for discount rule operations. */
export function assertDiscountAccess(tenantId: string, action: DiscountAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("discount access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`discount ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayDiscount(tenantId: string, action: DiscountAction): boolean {
  try {
    assertDiscountAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): DiscountAction[] {
  const actions: DiscountAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayDiscount(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("discount belongs to a different tenant");
  }
}
