import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type UsageAction = "read" | "write" | "administer";

const required: Record<UsageAction, readonly string[]> = {
  read: ["usage:read"],
  write: ["usage:read", "usage:write"],
  administer: ["usage:read", "usage:write", "usage:admin"],
};

/** Tenant-scoped authorization for metered usage record operations. */
export function assertUsageAccess(tenantId: string, action: UsageAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("usage access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`usage ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayUsage(tenantId: string, action: UsageAction): boolean {
  try {
    assertUsageAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): UsageAction[] {
  const actions: UsageAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayUsage(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("usage belongs to a different tenant");
  }
}
