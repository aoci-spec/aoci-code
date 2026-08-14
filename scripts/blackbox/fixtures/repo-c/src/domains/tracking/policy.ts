import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type TrackingAction = "read" | "write" | "administer";

const required: Record<TrackingAction, readonly string[]> = {
  read: ["tracking:read"],
  write: ["tracking:read", "tracking:write"],
  administer: ["tracking:read", "tracking:write", "tracking:admin"],
};

/** Tenant-scoped authorization for tracking event operations. */
export function assertTrackingAccess(tenantId: string, action: TrackingAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("tracking access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`tracking ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayTracking(tenantId: string, action: TrackingAction): boolean {
  try {
    assertTrackingAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): TrackingAction[] {
  const actions: TrackingAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayTracking(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("tracking belongs to a different tenant");
  }
}
