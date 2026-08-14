import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type WarrantyAction = "read" | "write" | "administer";

const required: Record<WarrantyAction, readonly string[]> = {
  read: ["warranty:read"],
  write: ["warranty:read", "warranty:write"],
  administer: ["warranty:read", "warranty:write", "warranty:admin"],
};

/** Tenant-scoped authorization for warranty claim operations. */
export function assertWarrantyAccess(tenantId: string, action: WarrantyAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("warranty access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`warranty ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayWarranty(tenantId: string, action: WarrantyAction): boolean {
  try {
    assertWarrantyAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): WarrantyAction[] {
  const actions: WarrantyAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayWarranty(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("warranty belongs to a different tenant");
  }
}
