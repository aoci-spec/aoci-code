import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type PaymentAction = "read" | "write" | "administer";

const required: Record<PaymentAction, readonly string[]> = {
  read: ["payment:read"],
  write: ["payment:read", "payment:write"],
  administer: ["payment:read", "payment:write", "payment:admin"],
};

/** Tenant-scoped authorization for payment attempt operations. */
export function assertPaymentAccess(tenantId: string, action: PaymentAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("payment access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`payment ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayPayment(tenantId: string, action: PaymentAction): boolean {
  try {
    assertPaymentAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): PaymentAction[] {
  const actions: PaymentAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayPayment(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("payment belongs to a different tenant");
  }
}
