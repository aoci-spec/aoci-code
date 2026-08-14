import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type AddressAction = "read" | "write" | "administer";

const required: Record<AddressAction, readonly string[]> = {
  read: ["address:read"],
  write: ["address:read", "address:write"],
  administer: ["address:read", "address:write", "address:admin"],
};

/** Tenant-scoped authorization for postal address operations. */
export function assertAddressAccess(tenantId: string, action: AddressAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("address access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`address ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayAddress(tenantId: string, action: AddressAction): boolean {
  try {
    assertAddressAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): AddressAction[] {
  const actions: AddressAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayAddress(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("address belongs to a different tenant");
  }
}
