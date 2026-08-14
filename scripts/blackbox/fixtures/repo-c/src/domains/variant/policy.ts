import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type VariantAction = "read" | "write" | "administer";

const required: Record<VariantAction, readonly string[]> = {
  read: ["variant:read"],
  write: ["variant:read", "variant:write"],
  administer: ["variant:read", "variant:write", "variant:admin"],
};

/** Tenant-scoped authorization for product variant operations. */
export function assertVariantAccess(tenantId: string, action: VariantAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("variant access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`variant ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayVariant(tenantId: string, action: VariantAction): boolean {
  try {
    assertVariantAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): VariantAction[] {
  const actions: VariantAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayVariant(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("variant belongs to a different tenant");
  }
}
