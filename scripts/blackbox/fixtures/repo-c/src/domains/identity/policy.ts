import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type IdentityAction = "read" | "write" | "administer";

const required: Record<IdentityAction, readonly string[]> = {
  read: ["identity:read"],
  write: ["identity:read", "identity:write"],
  administer: ["identity:read", "identity:write", "identity:admin"],
};

/** Tenant-scoped authorization for identity record operations. */
export function assertIdentityAccess(tenantId: string, action: IdentityAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("identity access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`identity ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayIdentity(tenantId: string, action: IdentityAction): boolean {
  try {
    assertIdentityAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): IdentityAction[] {
  const actions: IdentityAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayIdentity(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("identity belongs to a different tenant");
  }
}
