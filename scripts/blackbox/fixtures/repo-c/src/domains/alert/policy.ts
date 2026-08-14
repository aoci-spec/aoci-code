import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type AlertAction = "read" | "write" | "administer";

const required: Record<AlertAction, readonly string[]> = {
  read: ["alert:read"],
  write: ["alert:read", "alert:write"],
  administer: ["alert:read", "alert:write", "alert:admin"],
};

/** Tenant-scoped authorization for operational alert operations. */
export function assertAlertAccess(tenantId: string, action: AlertAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("alert access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`alert ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayAlert(tenantId: string, action: AlertAction): boolean {
  try {
    assertAlertAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): AlertAction[] {
  const actions: AlertAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayAlert(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("alert belongs to a different tenant");
  }
}
