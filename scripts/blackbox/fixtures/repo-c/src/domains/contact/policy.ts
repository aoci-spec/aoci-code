import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type ContactAction = "read" | "write" | "administer";

const required: Record<ContactAction, readonly string[]> = {
  read: ["contact:read"],
  write: ["contact:read", "contact:write"],
  administer: ["contact:read", "contact:write", "contact:admin"],
};

/** Tenant-scoped authorization for contact person operations. */
export function assertContactAccess(tenantId: string, action: ContactAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("contact access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`contact ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayContact(tenantId: string, action: ContactAction): boolean {
  try {
    assertContactAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): ContactAction[] {
  const actions: ContactAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayContact(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("contact belongs to a different tenant");
  }
}
