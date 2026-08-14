import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type ReturnCaseAction = "read" | "write" | "administer";

const required: Record<ReturnCaseAction, readonly string[]> = {
  read: ["return:read"],
  write: ["return:read", "return:write"],
  administer: ["return:read", "return:write", "return:admin"],
};

/** Tenant-scoped authorization for return case operations. */
export function assertReturnCaseAccess(tenantId: string, action: ReturnCaseAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("return access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`return ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayReturnCase(tenantId: string, action: ReturnCaseAction): boolean {
  try {
    assertReturnCaseAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): ReturnCaseAction[] {
  const actions: ReturnCaseAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayReturnCase(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("return belongs to a different tenant");
  }
}
