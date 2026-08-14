import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type TenantAction = "read" | "write" | "administer";

const required: Record<TenantAction, readonly string[]> = {
  read: ["tenant:read"],
  write: ["tenant:read", "tenant:write"],
  administer: ["tenant:read", "tenant:write", "tenant:admin"],
};

/** Tenant-scoped authorization for tenant boundary operations. */
export function assertTenantAccess(tenantId: string, action: TenantAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("tenant access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`tenant ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayTenant(tenantId: string, action: TenantAction): boolean {
  try {
    assertTenantAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): TenantAction[] {
  const actions: TenantAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayTenant(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("tenant belongs to a different tenant");
  }
}
