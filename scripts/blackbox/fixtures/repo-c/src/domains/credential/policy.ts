import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type CredentialAction = "read" | "write" | "administer";

const required: Record<CredentialAction, readonly string[]> = {
  read: ["credential:read"],
  write: ["credential:read", "credential:write"],
  administer: ["credential:read", "credential:write", "credential:admin"],
};

/** Tenant-scoped authorization for stored credential operations. */
export function assertCredentialAccess(tenantId: string, action: CredentialAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("credential access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`credential ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayCredential(tenantId: string, action: CredentialAction): boolean {
  try {
    assertCredentialAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): CredentialAction[] {
  const actions: CredentialAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayCredential(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("credential belongs to a different tenant");
  }
}
