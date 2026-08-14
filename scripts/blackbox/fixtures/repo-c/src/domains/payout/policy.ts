import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type PayoutAction = "read" | "write" | "administer";

const required: Record<PayoutAction, readonly string[]> = {
  read: ["payout:read"],
  write: ["payout:read", "payout:write"],
  administer: ["payout:read", "payout:write", "payout:admin"],
};

/** Tenant-scoped authorization for merchant payout operations. */
export function assertPayoutAccess(tenantId: string, action: PayoutAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("payout access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`payout ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayPayout(tenantId: string, action: PayoutAction): boolean {
  try {
    assertPayoutAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): PayoutAction[] {
  const actions: PayoutAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayPayout(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("payout belongs to a different tenant");
  }
}
