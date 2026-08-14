import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type CatalogAction = "read" | "write" | "administer";

const required: Record<CatalogAction, readonly string[]> = {
  read: ["catalog:read"],
  write: ["catalog:read", "catalog:write"],
  administer: ["catalog:read", "catalog:write", "catalog:admin"],
};

/** Tenant-scoped authorization for product catalog operations. */
export function assertCatalogAccess(tenantId: string, action: CatalogAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("catalog access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`catalog ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayCatalog(tenantId: string, action: CatalogAction): boolean {
  try {
    assertCatalogAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): CatalogAction[] {
  const actions: CatalogAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayCatalog(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("catalog belongs to a different tenant");
  }
}
