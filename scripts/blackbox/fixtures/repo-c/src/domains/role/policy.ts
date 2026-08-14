import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type RoleAction = "read" | "write" | "administer";

const required: Record<RoleAction, readonly string[]> = {
  read: ["role:read"],
  write: ["role:read", "role:write"],
  administer: ["role:read", "role:write", "role:admin"],
};

/** Tenant-scoped authorization for authorization role operations. */
export function assertRoleAccess(tenantId: string, action: RoleAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("role access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`role ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayRole(tenantId: string, action: RoleAction): boolean {
  try {
    assertRoleAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): RoleAction[] {
  const actions: RoleAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayRole(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("role belongs to a different tenant");
  }
}
