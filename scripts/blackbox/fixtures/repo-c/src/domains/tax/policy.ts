import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type TaxAction = "read" | "write" | "administer";

const required: Record<TaxAction, readonly string[]> = {
  read: ["tax:read"],
  write: ["tax:read", "tax:write"],
  administer: ["tax:read", "tax:write", "tax:admin"],
};

/** Tenant-scoped authorization for tax determination operations. */
export function assertTaxAccess(tenantId: string, action: TaxAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("tax access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`tax ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayTax(tenantId: string, action: TaxAction): boolean {
  try {
    assertTaxAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): TaxAction[] {
  const actions: TaxAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayTax(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("tax belongs to a different tenant");
  }
}
