import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type FeatureAction = "read" | "write" | "administer";

const required: Record<FeatureAction, readonly string[]> = {
  read: ["feature:read"],
  write: ["feature:read", "feature:write"],
  administer: ["feature:read", "feature:write", "feature:admin"],
};

/** Tenant-scoped authorization for feature flag operations. */
export function assertFeatureAccess(tenantId: string, action: FeatureAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("feature access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`feature ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayFeature(tenantId: string, action: FeatureAction): boolean {
  try {
    assertFeatureAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): FeatureAction[] {
  const actions: FeatureAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayFeature(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("feature belongs to a different tenant");
  }
}
