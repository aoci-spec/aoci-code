import { ForbiddenError } from "../../infra/errors";
import { hasPermission } from "../../infra/permissions";

export type WebhookAction = "read" | "write" | "administer";

const required: Record<WebhookAction, readonly string[]> = {
  read: ["webhook:read"],
  write: ["webhook:read", "webhook:write"],
  administer: ["webhook:read", "webhook:write", "webhook:admin"],
};

/** Tenant-scoped authorization for outbound webhook operations. */
export function assertWebhookAccess(tenantId: string, action: WebhookAction): void {
  if (tenantId.trim().length === 0) {
    throw new ForbiddenError("webhook access requires a tenant header");
  }
  for (const permission of required[action]) {
    if (!hasPermission(tenantId, permission)) {
      throw new ForbiddenError(`webhook ${action} is not granted for tenant ${tenantId}`);
    }
  }
}

export function mayWebhook(tenantId: string, action: WebhookAction): boolean {
  try {
    assertWebhookAccess(tenantId, action);
    return true;
  } catch {
    return false;
  }
}

export function grantedActions(tenantId: string): WebhookAction[] {
  const actions: WebhookAction[] = ["read", "write", "administer"];
  return actions.filter((action) => mayWebhook(tenantId, action));
}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {
  if (tenantId !== resourceTenantId) {
    throw new ForbiddenError("webhook belongs to a different tenant");
  }
}
