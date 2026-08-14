import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type CustomerAction = "read" | "write" | "administer";

const required: Record<CustomerAction, readonly string[]> = {
  read: ["customer:read"],
  write: ["customer:read", "customer:write"],
  administer: ["customer:read", "customer:write", "customer:admin"],
};

/** Tenant-scoped authorization for customer account operations. */
export function assertCustomerAccess(tenantId: string, action: CustomerAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("customer access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`customer ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayCustomer(tenantId: string, action: CustomerAction): boolean {
  try {
    assertCustomerAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): CustomerAction[] {
  const actions: CustomerAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayCustomer(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("customer belongs to a different tenant");
  }
}
