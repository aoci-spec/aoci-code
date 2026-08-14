import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type FraudAction = "read" | "write" | "administer";

const required: Record<FraudAction, readonly string[]> = {
  read: ["fraud:read"],
  write: ["fraud:read", "fraud:write"],
  administer: ["fraud:read", "fraud:write", "fraud:admin"],
};

/** Tenant-scoped authorization for fraud signal operations. */
export function assertFraudAccess(tenantId: string, action: FraudAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("fraud access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`fraud ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayFraud(tenantId: string, action: FraudAction): boolean {
  try {
    assertFraudAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): FraudAction[] {
  const actions: FraudAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayFraud(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("fraud belongs to a different tenant");
  }
}
