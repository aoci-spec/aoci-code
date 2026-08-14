import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type RefundAction = "read" | "write" | "administer";

const required: Record<RefundAction, readonly string[]> = {
  read: ["refund:read"],
  write: ["refund:read", "refund:write"],
  administer: ["refund:read", "refund:write", "refund:admin"],
};

/** Tenant-scoped authorization for refund request operations. */
export function assertRefundAccess(tenantId: string, action: RefundAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("refund access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`refund ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayRefund(tenantId: string, action: RefundAction): boolean {
  try {
    assertRefundAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): RefundAction[] {
  const actions: RefundAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayRefund(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("refund belongs to a different tenant");
  }
}
