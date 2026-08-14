import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type ExportRunAction = "read" | "write" | "administer";

const required: Record<ExportRunAction, readonly string[]> = {
  read: ["export:read"],
  write: ["export:read", "export:write"],
  administer: ["export:read", "export:write", "export:admin"],
};

/** Tenant-scoped authorization for bulk export run operations. */
export function assertExportRunAccess(tenantId: string, action: ExportRunAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("export access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`export ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayExportRun(tenantId: string, action: ExportRunAction): boolean {
  try {
    assertExportRunAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): ExportRunAction[] {
  const actions: ExportRunAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayExportRun(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("export belongs to a different tenant");
  }
}
