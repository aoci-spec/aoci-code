import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type CurrencyAction = "read" | "write" | "administer";

const required: Record<CurrencyAction, readonly string[]> = {
  read: ["currency:read"],
  write: ["currency:read", "currency:write"],
  administer: ["currency:read", "currency:write", "currency:admin"],
};

/** Tenant-scoped authorization for currency rate operations. */
export function assertCurrencyAccess(tenantId: string, action: CurrencyAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("currency access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`currency ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayCurrency(tenantId: string, action: CurrencyAction): boolean {
  try {
    assertCurrencyAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): CurrencyAction[] {
  const actions: CurrencyAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayCurrency(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("currency belongs to a different tenant");
  }
}
