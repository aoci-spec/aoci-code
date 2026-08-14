import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type InvoiceAction = "read" | "write" | "administer";

const required: Record<InvoiceAction, readonly string[]> = {
  read: ["invoice:read"],
  write: ["invoice:read", "invoice:write"],
  administer: ["invoice:read", "invoice:write", "invoice:admin"],
};

/** Tenant-scoped authorization for billing invoice operations. */
export function assertInvoiceAccess(tenantId: string, action: InvoiceAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("invoice access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`invoice ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayInvoice(tenantId: string, action: InvoiceAction): boolean {
  try {
    assertInvoiceAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): InvoiceAction[] {
  const actions: InvoiceAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayInvoice(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("invoice belongs to a different tenant");
  }
}
