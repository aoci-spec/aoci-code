import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type OrderAction = "read" | "write" | "administer";

const required: Record<OrderAction, readonly string[]> = {
  read: ["order:read"],
  write: ["order:read", "order:write"],
  administer: ["order:read", "order:write", "order:admin"],
};

/** Tenant-scoped authorization for purchase order operations. */
export function assertOrderAccess(tenantId: string, action: OrderAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("order access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`order ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayOrder(tenantId: string, action: OrderAction): boolean {
  try {
    assertOrderAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): OrderAction[] {
  const actions: OrderAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayOrder(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("order belongs to a different tenant");
  }
}
