import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type SubscriptionAction = "read" | "write" | "administer";

const required: Record<SubscriptionAction, readonly string[]> = {
  read: ["subscription:read"],
  write: ["subscription:read", "subscription:write"],
  administer: ["subscription:read", "subscription:write", "subscription:admin"],
};

/** Tenant-scoped authorization for recurring subscription operations. */
export function assertSubscriptionAccess(tenantId: string, action: SubscriptionAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("subscription access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`subscription ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function maySubscription(tenantId: string, action: SubscriptionAction): boolean {
  try {
    assertSubscriptionAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): SubscriptionAction[] {
  const actions: SubscriptionAction[] = ["read", "write", "administer"];
  return actions.filter((action) => maySubscription(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("subscription belongs to a different tenant");
  }
}
