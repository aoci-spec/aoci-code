import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type ProductAction = "read" | "write" | "administer";

const required: Record<ProductAction, readonly string[]> = {
  read: ["product:read"],
  write: ["product:read", "product:write"],
  administer: ["product:read", "product:write", "product:admin"],
};

/** Tenant-scoped authorization for sellable product operations. */
export function assertProductAccess(tenantId: string, action: ProductAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("product access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`product ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayProduct(tenantId: string, action: ProductAction): boolean {
  try {
    assertProductAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): ProductAction[] {
  const actions: ProductAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayProduct(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("product belongs to a different tenant");
  }
}
