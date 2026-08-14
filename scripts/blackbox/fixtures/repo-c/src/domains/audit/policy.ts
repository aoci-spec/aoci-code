import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type AuditAction = "read" | "write" | "administer";

const required: Record<AuditAction, readonly string[]> = {
  read: ["audit:read"],
  write: ["audit:read", "audit:write"],
  administer: ["audit:read", "audit:write", "audit:admin"],
};

/** Tenant-scoped authorization for audit trail record operations. */
export function assertAuditAccess(tenantId: string, action: AuditAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("audit access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`audit ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayAudit(tenantId: string, action: AuditAction): boolean {
  try {
    assertAuditAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): AuditAction[] {
  const actions: AuditAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayAudit(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("audit belongs to a different tenant");
  }
}
