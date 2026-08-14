import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type ImportRunAction = "read" | "write" | "administer";

const required: Record<ImportRunAction, readonly string[]> = {
  read: ["import:read"],
  write: ["import:read", "import:write"],
  administer: ["import:read", "import:write", "import:admin"],
};

/** Tenant-scoped authorization for bulk import run operations. */
export function assertImportRunAccess(tenantId: string, action: ImportRunAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("import access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`import ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayImportRun(tenantId: string, action: ImportRunAction): boolean {
  try {
    assertImportRunAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): ImportRunAction[] {
  const actions: ImportRunAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayImportRun(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("import belongs to a different tenant");
  }
}
