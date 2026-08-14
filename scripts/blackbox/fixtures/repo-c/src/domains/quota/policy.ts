import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type QuotaAction = "read" | "write" | "administer";

const required: Record<QuotaAction, readonly string[]> = {
  read: ["quota:read"],
  write: ["quota:read", "quota:write"],
  administer: ["quota:read", "quota:write", "quota:admin"],
};

/** Tenant-scoped authorization for consumption quota operations. */
export function assertQuotaAccess(tenantId: string, action: QuotaAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("quota access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`quota ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayQuota(tenantId: string, action: QuotaAction): boolean {
  try {
    assertQuotaAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): QuotaAction[] {
  const actions: QuotaAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayQuota(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("quota belongs to a different tenant");
  }
}
