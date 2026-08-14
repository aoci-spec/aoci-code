import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type PermissionAction = "read" | "write" | "administer";

const required: Record<PermissionAction, readonly string[]> = {
  read: ["permission:read"],
  write: ["permission:read", "permission:write"],
  administer: ["permission:read", "permission:write", "permission:admin"],
};

/** Tenant-scoped authorization for permission grant operations. */
export function assertPermissionAccess(tenantId: string, action: PermissionAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("permission access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`permission ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayPermission(tenantId: string, action: PermissionAction): boolean {
  try {
    assertPermissionAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): PermissionAction[] {
  const actions: PermissionAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayPermission(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("permission belongs to a different tenant");
  }
}
