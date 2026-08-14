import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type ApprovalAction = "read" | "write" | "administer";

const required: Record<ApprovalAction, readonly string[]> = {
  read: ["approval:read"],
  write: ["approval:read", "approval:write"],
  administer: ["approval:read", "approval:write", "approval:admin"],
};

/** Tenant-scoped authorization for approval decision operations. */
export function assertApprovalAccess(tenantId: string, action: ApprovalAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("approval access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`approval ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayApproval(tenantId: string, action: ApprovalAction): boolean {
  try {
    assertApprovalAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): ApprovalAction[] {
  const actions: ApprovalAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayApproval(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("approval belongs to a different tenant");
  }
}
