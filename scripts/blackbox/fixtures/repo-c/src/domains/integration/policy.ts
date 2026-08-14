import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type IntegrationAction = "read" | "write" | "administer";

const required: Record<IntegrationAction, readonly string[]> = {
  read: ["integration:read"],
  write: ["integration:read", "integration:write"],
  administer: ["integration:read", "integration:write", "integration:admin"],
};

/** Tenant-scoped authorization for external integration operations. */
export function assertIntegrationAccess(tenantId: string, action: IntegrationAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("integration access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`integration ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayIntegration(tenantId: string, action: IntegrationAction): boolean {
  try {
    assertIntegrationAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): IntegrationAction[] {
  const actions: IntegrationAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayIntegration(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("integration belongs to a different tenant");
  }
}
