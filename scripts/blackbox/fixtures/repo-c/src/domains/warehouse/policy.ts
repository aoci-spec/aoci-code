import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type WarehouseAction = "read" | "write" | "administer";

const required: Record<WarehouseAction, readonly string[]> = {
  read: ["warehouse:read"],
  write: ["warehouse:read", "warehouse:write"],
  administer: ["warehouse:read", "warehouse:write", "warehouse:admin"],
};

/** Tenant-scoped authorization for storage facility operations. */
export function assertWarehouseAccess(tenantId: string, action: WarehouseAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("warehouse access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`warehouse ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayWarehouse(tenantId: string, action: WarehouseAction): boolean {
  try {
    assertWarehouseAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): WarehouseAction[] {
  const actions: WarehouseAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayWarehouse(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("warehouse belongs to a different tenant");
  }
}
