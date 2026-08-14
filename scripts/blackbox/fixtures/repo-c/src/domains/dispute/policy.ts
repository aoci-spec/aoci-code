import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type DisputeAction = "read" | "write" | "administer";

const required: Record<DisputeAction, readonly string[]> = {
  read: ["dispute:read"],
  write: ["dispute:read", "dispute:write"],
  administer: ["dispute:read", "dispute:write", "dispute:admin"],
};

/** Tenant-scoped authorization for payment dispute operations. */
export function assertDisputeAccess(tenantId: string, action: DisputeAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("dispute access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`dispute ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayDispute(tenantId: string, action: DisputeAction): boolean {
  try {
    assertDisputeAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): DisputeAction[] {
  const actions: DisputeAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayDispute(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("dispute belongs to a different tenant");
  }
}
