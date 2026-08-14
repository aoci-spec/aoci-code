import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type ConsentAction = "read" | "write" | "administer";

const required: Record<ConsentAction, readonly string[]> = {
  read: ["consent:read"],
  write: ["consent:read", "consent:write"],
  administer: ["consent:read", "consent:write", "consent:admin"],
};

/** Tenant-scoped authorization for privacy consent operations. */
export function assertConsentAccess(tenantId: string, action: ConsentAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("consent access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`consent ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayConsent(tenantId: string, action: ConsentAction): boolean {
  try {
    assertConsentAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): ConsentAction[] {
  const actions: ConsentAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayConsent(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("consent belongs to a different tenant");
  }
}
