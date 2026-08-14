import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type InventoryAction = "read" | "write" | "administer";

const required: Record<InventoryAction, readonly string[]> = {
  read: ["inventory:read"],
  write: ["inventory:read", "inventory:write"],
  administer: ["inventory:read", "inventory:write", "inventory:admin"],
};

/** Tenant-scoped authorization for stock position operations. */
export function assertInventoryAccess(tenantId: string, action: InventoryAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("inventory access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`inventory ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayInventory(tenantId: string, action: InventoryAction): boolean {
  try {
    assertInventoryAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): InventoryAction[] {
  const actions: InventoryAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayInventory(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("inventory belongs to a different tenant");
  }
}
