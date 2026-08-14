import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type SessionAction = "read" | "write" | "administer";

const required: Record<SessionAction, readonly string[]> = {
  read: ["session:read"],
  write: ["session:read", "session:write"],
  administer: ["session:read", "session:write", "session:admin"],
};

/** Tenant-scoped authorization for authenticated session operations. */
export function assertSessionAccess(tenantId: string, action: SessionAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("session access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`session ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function maySession(tenantId: string, action: SessionAction): boolean {
  try {
    assertSessionAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): SessionAction[] {
  const actions: SessionAction[] = ["read", "write", "administer"];
  return actions.filter((action) => maySession(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("session belongs to a different tenant");
  }
}
