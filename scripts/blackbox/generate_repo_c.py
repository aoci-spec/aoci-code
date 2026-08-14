#!/usr/bin/env python3
"""生成冻结夹具 repo-c —— 规模化多批与关系闭包的黑盒对象。

repo-a/repo-b 只有二十余个文件,一批就装完;单批上限(200)之下的**多批滚动**与
**关系闭包重排**因此从未被跨进程测过。repo-c 提供约 450 个真实分层的 TypeScript
文件,让这两条路径在真实上限下可测。

重要: 本脚本只用于**有意重建**夹具。夹具一旦冻结进仓,其文件数与字节即成为
scale 场景的断言基准;重新生成会改变夹具身份,必须是明确决定并同步更新断言。

    python3 scripts/blackbox/generate_repo_c.py            # 写入 fixtures/repo-c
    python3 scripts/blackbox/generate_repo_c.py --check    # 只校验现有夹具规模
"""
import argparse, hashlib, os, shutil, sys

HERE = os.path.dirname(os.path.abspath(__file__))
TARGET = os.path.join(HERE, "fixtures", "repo-c")

# 60 个业务域 × 7 层 = 420, 加 30 个基础设施文件 ≈ 450。
DOMAINS = [
    ("order", "Order", "purchase order"), ("invoice", "Invoice", "billing invoice"),
    ("payment", "Payment", "payment attempt"), ("refund", "Refund", "refund request"),
    ("customer", "Customer", "customer account"), ("contact", "Contact", "contact person"),
    ("address", "Address", "postal address"), ("catalog", "Catalog", "product catalog"),
    ("product", "Product", "sellable product"), ("variant", "Variant", "product variant"),
    ("price", "Price", "price definition"), ("discount", "Discount", "discount rule"),
    ("coupon", "Coupon", "coupon grant"), ("inventory", "Inventory", "stock position"),
    ("warehouse", "Warehouse", "storage facility"), ("shipment", "Shipment", "outbound shipment"),
    ("carrier", "Carrier", "delivery carrier"), ("tracking", "Tracking", "tracking event"),
    ("return", "ReturnCase", "return case"), ("warranty", "Warranty", "warranty claim"),
    ("ticket", "Ticket", "support ticket"), ("message", "Message", "customer message"),
    ("notification", "Notification", "outbound notification"), ("template", "Template", "message template"),
    ("subscription", "Subscription", "recurring subscription"), ("plan", "Plan", "subscription plan"),
    ("usage", "Usage", "metered usage record"), ("quota", "Quota", "consumption quota"),
    ("ledger", "Ledger", "accounting ledger entry"), ("journal", "Journal", "journal batch"),
    ("tax", "Tax", "tax determination"), ("currency", "Currency", "currency rate"),
    ("payout", "Payout", "merchant payout"), ("settlement", "Settlement", "settlement run"),
    ("dispute", "Dispute", "payment dispute"), ("fraud", "Fraud", "fraud signal"),
    ("identity", "Identity", "identity record"), ("session", "Session", "authenticated session"),
    ("credential", "Credential", "stored credential"), ("permission", "Permission", "permission grant"),
    ("role", "Role", "authorization role"), ("tenant", "Tenant", "tenant boundary"),
    ("audit", "Audit", "audit trail record"), ("consent", "Consent", "privacy consent"),
    ("document", "Document", "stored document"), ("attachment", "Attachment", "file attachment"),
    ("workflow", "Workflow", "workflow instance"), ("task", "Task", "workflow task"),
    ("approval", "Approval", "approval decision"), ("schedule", "Schedule", "scheduled run"),
    ("job", "Job", "background job"), ("webhook", "Webhook", "outbound webhook"),
    ("integration", "Integration", "external integration"), ("import", "ImportRun", "bulk import run"),
    ("export", "ExportRun", "bulk export run"), ("report", "Report", "generated report"),
    ("metric", "Metric", "aggregated metric"), ("alert", "Alert", "operational alert"),
    ("feature", "Feature", "feature flag"), ("setting", "Setting", "tenant setting"),
]

LAYERS = ["model", "repository", "service", "handler", "validator", "mapper", "policy"]


def model_file(key, name, human):
    return f"""import {{ isoTimestamp, isBefore }} from "../../infra/time";
import {{ ValidationError }} from "../../infra/errors";

/** Durable shape of one {human}. */
export interface {name} {{
  readonly id: string;
  readonly tenantId: string;
  status: {name}Status;
  amountCents: number;
  reference: string;
  labels: readonly string[];
  history: readonly {name}Change[];
  readonly createdAt: string;
  updatedAt: string;
}}

export interface {name}Change {{
  readonly at: string;
  readonly from: {name}Status;
  readonly to: {name}Status;
}}

export type {name}Status = "draft" | "active" | "settled" | "cancelled";

export const {key}Statuses: readonly {name}Status[] = ["draft", "active", "settled", "cancelled"];

/** Legal forward transitions for a {human}; anything else is rejected upstream. */
const transitions: Record<{name}Status, readonly {name}Status[]> = {{
  draft: ["active", "cancelled"],
  active: ["settled", "cancelled"],
  settled: [],
  cancelled: [],
}};

export function can{name}Transition(from: {name}Status, to: {name}Status): boolean {{
  return transitions[from].includes(to);
}}

export function is{name}Terminal(value: {name}): boolean {{
  return transitions[value.status].length === 0;
}}

export function new{name}(id: string, tenantId: string, reference: string): {name} {{
  const now = isoTimestamp();
  return {{
    id,
    tenantId,
    status: "draft",
    amountCents: 0,
    reference,
    labels: [],
    history: [],
    createdAt: now,
    updatedAt: now,
  }};
}}

export function touch{name}(value: {name}): {name} {{
  return {{ ...value, updatedAt: isoTimestamp() }};
}}

/** Applies a transition and records it; callers must check legality first. */
export function apply{name}Transition(value: {name}, to: {name}Status): {name} {{
  const change: {name}Change = {{ at: isoTimestamp(), from: value.status, to }};
  return {{
    ...value,
    status: to,
    history: [...value.history, change],
    updatedAt: change.at,
  }};
}}

export function with{name}Amount(value: {name}, amountCents: number): {name} {{
  if (!Number.isInteger(amountCents) || amountCents < 0) {{
    throw new ValidationError("{key} amount must be a non-negative integer");
  }}
  return touch{name}({{ ...value, amountCents }});
}}

export function with{name}Label(value: {name}, label: string): {name} {{
  if (label.trim().length === 0) {{
    throw new ValidationError("{key} label must not be blank");
  }}
  if (value.labels.includes(label)) {{
    return value;
  }}
  return touch{name}({{ ...value, labels: [...value.labels, label].sort() }});
}}

export function without{name}Label(value: {name}, label: string): {name} {{
  if (!value.labels.includes(label)) {{
    return value;
  }}
  return touch{name}({{ ...value, labels: value.labels.filter((item) => item !== label) }});
}}

export function validate{name}(value: {name}): void {{
  if (value.id.length === 0 || value.tenantId.length === 0) {{
    throw new ValidationError("{key} requires both id and tenantId");
  }}
  if (value.reference.trim().length === 0) {{
    throw new ValidationError("{key} reference must not be blank");
  }}
  if (!Number.isInteger(value.amountCents) || value.amountCents < 0) {{
    throw new ValidationError("{key} amount must be a non-negative integer");
  }}
  if (isBefore(value.updatedAt, value.createdAt)) {{
    throw new ValidationError("{key} updatedAt precedes createdAt");
  }}
}}

export function compare{name}(left: {name}, right: {name}): number {{
  const byCreation = left.createdAt.localeCompare(right.createdAt);
  return byCreation !== 0 ? byCreation : left.id.localeCompare(right.id);
}}

export function summarize{name}(value: {name}): string {{
  return `${{value.reference}} (${{value.status}}, ${{(value.amountCents / 100).toFixed(2)}})`;
}}

export function {key}StatusCounts(values: readonly {name}[]): Record<{name}Status, number> {{
  const counts: Record<{name}Status, number> = {{ draft: 0, active: 0, settled: 0, cancelled: 0 }};
  for (const value of values) {{
    counts[value.status] += 1;
  }}
  return counts;
}}
"""


def repository_file(key, name, human):
    return f"""import {{ {name}, {name}Status, compare{name}, touch{name}, validate{name} }} from "./model";
import {{ ConflictError, NotFoundError }} from "../../infra/errors";

export interface {name}Page {{
  readonly items: readonly {name}[];
  readonly total: number;
  readonly offset: number;
}}

/** In-memory store for {human} records, keyed by tenant and id. */
export class {name}Repository {{
  private readonly byTenant = new Map<string, Map<string, {name}>>();
  private readonly byReference = new Map<string, string>();

  private tenantMap(tenantId: string): Map<string, {name}> {{
    const existing = this.byTenant.get(tenantId);
    if (existing) {{
      return existing;
    }}
    const created = new Map<string, {name}>();
    this.byTenant.set(tenantId, created);
    return created;
  }}

  private referenceKey(tenantId: string, reference: string): string {{
    return `${{tenantId}}::${{reference}}`;
  }}

  insert(value: {name}): {name} {{
    validate{name}(value);
    const tenant = this.tenantMap(value.tenantId);
    if (tenant.has(value.id)) {{
      throw new ConflictError(`{key} ${{value.id}} already exists`);
    }}
    const referenceKey = this.referenceKey(value.tenantId, value.reference);
    if (this.byReference.has(referenceKey)) {{
      throw new ConflictError(`{key} reference ${{value.reference}} is already used`);
    }}
    tenant.set(value.id, value);
    this.byReference.set(referenceKey, value.id);
    return value;
  }}

  save(value: {name}): {name} {{
    validate{name}(value);
    const tenant = this.tenantMap(value.tenantId);
    const previous = tenant.get(value.id);
    if (previous && previous.reference !== value.reference) {{
      this.byReference.delete(this.referenceKey(value.tenantId, previous.reference));
      this.byReference.set(this.referenceKey(value.tenantId, value.reference), value.id);
    }}
    const stored = touch{name}(value);
    tenant.set(stored.id, stored);
    return stored;
  }}

  find(tenantId: string, id: string): {name} | undefined {{
    return this.byTenant.get(tenantId)?.get(id);
  }}

  findByReference(tenantId: string, reference: string): {name} | undefined {{
    const id = this.byReference.get(this.referenceKey(tenantId, reference));
    return id === undefined ? undefined : this.find(tenantId, id);
  }}

  require(tenantId: string, id: string): {name} {{
    const found = this.find(tenantId, id);
    if (!found) {{
      throw new NotFoundError(`{key} ${{id}} not found for tenant ${{tenantId}}`);
    }}
    return found;
  }}

  listByStatus(tenantId: string, status: {name}Status): {name}[] {{
    return this.all(tenantId).filter((item) => item.status === status);
  }}

  listByLabel(tenantId: string, label: string): {name}[] {{
    return this.all(tenantId).filter((item) => item.labels.includes(label));
  }}

  page(tenantId: string, offset: number, limit: number): {name}Page {{
    const all = this.all(tenantId);
    const start = Math.max(0, offset);
    return {{ items: all.slice(start, start + Math.max(0, limit)), total: all.length, offset: start }};
  }}

  all(tenantId: string): {name}[] {{
    return [...(this.byTenant.get(tenantId)?.values() ?? [])].sort(compare{name});
  }}

  remove(tenantId: string, id: string): boolean {{
    const found = this.find(tenantId, id);
    if (!found) {{
      return false;
    }}
    this.byReference.delete(this.referenceKey(tenantId, found.reference));
    return this.byTenant.get(tenantId)?.delete(id) ?? false;
  }}

  count(tenantId: string): number {{
    return this.byTenant.get(tenantId)?.size ?? 0;
  }}

  clearTenant(tenantId: string): void {{
    for (const value of this.all(tenantId)) {{
      this.byReference.delete(this.referenceKey(tenantId, value.reference));
    }}
    this.byTenant.delete(tenantId);
  }}
}}
"""


def service_file(key, name, human):
    return f"""import {{
  {name},
  {name}Status,
  apply{name}Transition,
  can{name}Transition,
  is{name}Terminal,
  new{name},
  with{name}Amount,
  with{name}Label,
  {key}StatusCounts,
}} from "./model";
import {{ {name}Page, {name}Repository }} from "./repository";
import {{ IllegalTransitionError, ValidationError }} from "../../infra/errors";
import {{ auditEvent }} from "../../infra/audit";

export interface {name}Summary {{
  readonly total: number;
  readonly outstanding: number;
  readonly amountCents: number;
  readonly byStatus: Record<{name}Status, number>;
}}

/** Business rules for the {human} lifecycle. */
export class {name}Service {{
  constructor(private readonly repository: {name}Repository) {{}}

  create(tenantId: string, id: string, reference: string, amountCents: number): {name} {{
    const draft = with{name}Amount(new{name}(id, tenantId, reference), amountCents);
    const saved = this.repository.insert(draft);
    auditEvent("{key}.created", {{ tenantId, id }});
    return saved;
  }}

  get(tenantId: string, id: string): {name} {{
    return this.repository.require(tenantId, id);
  }}

  transition(tenantId: string, id: string, next: {name}Status): {name} {{
    const current = this.repository.require(tenantId, id);
    if (is{name}Terminal(current)) {{
      throw new IllegalTransitionError(`{key} ${{id}} is terminal`);
    }}
    if (!can{name}Transition(current.status, next)) {{
      throw new IllegalTransitionError(`{key} ${{id}}: ${{current.status}} -> ${{next}}`);
    }}
    const saved = this.repository.save(apply{name}Transition(current, next));
    auditEvent("{key}.transitioned", {{ tenantId, id, next }});
    return saved;
  }}

  adjustAmount(tenantId: string, id: string, deltaCents: number): {name} {{
    const current = this.repository.require(tenantId, id);
    if (is{name}Terminal(current)) {{
      throw new IllegalTransitionError(`{key} ${{id}} is terminal`);
    }}
    const amountCents = current.amountCents + deltaCents;
    if (amountCents < 0) {{
      throw new ValidationError(`{key} ${{id}} cannot fall below zero`);
    }}
    return this.repository.save(with{name}Amount(current, amountCents));
  }}

  label(tenantId: string, id: string, label: string): {name} {{
    return this.repository.save(with{name}Label(this.repository.require(tenantId, id), label));
  }}

  cancelAllDrafts(tenantId: string): number {{
    const drafts = this.repository.listByStatus(tenantId, "draft");
    for (const draft of drafts) {{
      this.repository.save(apply{name}Transition(draft, "cancelled"));
    }}
    if (drafts.length > 0) {{
      auditEvent("{key}.drafts_cancelled", {{ tenantId, count: drafts.length }});
    }}
    return drafts.length;
  }}

  outstanding(tenantId: string): {name}[] {{
    return [
      ...this.repository.listByStatus(tenantId, "draft"),
      ...this.repository.listByStatus(tenantId, "active"),
    ];
  }}

  page(tenantId: string, offset: number, limit: number): {name}Page {{
    return this.repository.page(tenantId, offset, limit);
  }}

  summary(tenantId: string): {name}Summary {{
    const all = this.repository.all(tenantId);
    return {{
      total: all.length,
      outstanding: this.outstanding(tenantId).length,
      amountCents: all.reduce((sum, item) => sum + item.amountCents, 0),
      byStatus: {key}StatusCounts(all),
    }};
  }}

  discard(tenantId: string, id: string): void {{
    const current = this.repository.require(tenantId, id);
    if (!is{name}Terminal(current)) {{
      throw new IllegalTransitionError(`{key} ${{id}} must reach a terminal status first`);
    }}
    this.repository.remove(tenantId, id);
    auditEvent("{key}.discarded", {{ tenantId, id }});
  }}
}}
"""


def handler_file(key, name, human):
    return f"""import {{ NextFunction, Request, Response }} from "express";
import {{ {name}Service }} from "./service";
import {{
  parse{name}Create,
  parse{name}Label,
  parse{name}Page,
  parse{name}Transition,
}} from "./validator";
import {{ to{name}PagePayload, to{name}Payload, to{name}SummaryPayload }} from "./mapper";
import {{ assert{name}Access }} from "./policy";

function tenantOf(request: Request): string {{
  return String(request.header("x-tenant") ?? "");
}}

/** HTTP surface for {human} resources. */
export function make{name}Handlers(service: {name}Service) {{
  return {{
    create(request: Request, response: Response, next: NextFunction): void {{
      try {{
        const tenantId = tenantOf(request);
        assert{name}Access(tenantId, "write");
        const input = parse{name}Create(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(to{name}Payload(created));
      }} catch (error) {{
        next(error);
      }}
    }},

    get(request: Request, response: Response, next: NextFunction): void {{
      try {{
        const tenantId = tenantOf(request);
        assert{name}Access(tenantId, "read");
        response.json(to{name}Payload(service.get(tenantId, String(request.params.id))));
      }} catch (error) {{
        next(error);
      }}
    }},

    transition(request: Request, response: Response, next: NextFunction): void {{
      try {{
        const tenantId = tenantOf(request);
        assert{name}Access(tenantId, "write");
        const input = parse{name}Transition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(to{name}Payload(updated));
      }} catch (error) {{
        next(error);
      }}
    }},

    label(request: Request, response: Response, next: NextFunction): void {{
      try {{
        const tenantId = tenantOf(request);
        assert{name}Access(tenantId, "write");
        const input = parse{name}Label(request.body);
        response.json(to{name}Payload(service.label(tenantId, String(request.params.id), input.label)));
      }} catch (error) {{
        next(error);
      }}
    }},

    list(request: Request, response: Response, next: NextFunction): void {{
      try {{
        const tenantId = tenantOf(request);
        assert{name}Access(tenantId, "read");
        const page = parse{name}Page(request.query);
        response.json(to{name}PagePayload(service.page(tenantId, page.offset, page.limit)));
      }} catch (error) {{
        next(error);
      }}
    }},

    outstanding(request: Request, response: Response, next: NextFunction): void {{
      try {{
        const tenantId = tenantOf(request);
        assert{name}Access(tenantId, "read");
        response.json(service.outstanding(tenantId).map(to{name}Payload));
      }} catch (error) {{
        next(error);
      }}
    }},

    summary(request: Request, response: Response, next: NextFunction): void {{
      try {{
        const tenantId = tenantOf(request);
        assert{name}Access(tenantId, "read");
        response.json(to{name}SummaryPayload(service.summary(tenantId)));
      }} catch (error) {{
        next(error);
      }}
    }},

    discard(request: Request, response: Response, next: NextFunction): void {{
      try {{
        const tenantId = tenantOf(request);
        assert{name}Access(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      }} catch (error) {{
        next(error);
      }}
    }},
  }};
}}
"""


def validator_file(key, name, human):
    return f"""import {{ {name}Status, {key}Statuses }} from "./model";
import {{ ValidationError }} from "../../infra/errors";

export interface {name}CreateInput {{
  id: string;
  reference: string;
  amountCents: number;
}}

export interface {name}TransitionInput {{
  status: {name}Status;
}}

export interface {name}LabelInput {{
  label: string;
}}

export interface {name}PageInput {{
  offset: number;
  limit: number;
}}

function requireRecord(body: unknown, what: string): Record<string, unknown> {{
  if (typeof body !== "object" || body === null || Array.isArray(body)) {{
    throw new ValidationError(`{key} ${{what}} body must be an object`);
  }}
  return body as Record<string, unknown>;
}}

function requireText(value: unknown, field: string, max = 190): string {{
  if (typeof value !== "string" || value.trim().length === 0) {{
    throw new ValidationError(`{key}.${{field}} must be a non-empty string`);
  }}
  if (value.length > max) {{
    throw new ValidationError(`{key}.${{field}} must be at most ${{max}} characters`);
  }}
  return value;
}}

/** Request-shape validation for {human} writes; never trusts client types. */
export function parse{name}Create(body: unknown): {name}CreateInput {{
  const record = requireRecord(body, "create");
  const id = requireText(record.id, "id", 64);
  const reference = requireText(record.reference, "reference");
  const amountCents = record.amountCents ?? 0;
  if (typeof amountCents !== "number" || !Number.isInteger(amountCents) || amountCents < 0) {{
    throw new ValidationError("{key}.amountCents must be a non-negative integer");
  }}
  return {{ id, reference, amountCents }};
}}

export function parse{name}Transition(body: unknown): {name}TransitionInput {{
  const record = requireRecord(body, "transition");
  const status = record.status;
  if (typeof status !== "string" || !{key}Statuses.includes(status as {name}Status)) {{
    throw new ValidationError(`{key}.status must be one of ${{{key}Statuses.join(", ")}}`);
  }}
  return {{ status: status as {name}Status }};
}}

export function parse{name}Label(body: unknown): {name}LabelInput {{
  const record = requireRecord(body, "label");
  return {{ label: requireText(record.label, "label", 40) }};
}}

export function parse{name}Page(query: unknown): {name}PageInput {{
  const record = requireRecord(query ?? {{}}, "page");
  const offset = record.offset === undefined ? 0 : Number(record.offset);
  const limit = record.limit === undefined ? 50 : Number(record.limit);
  if (!Number.isInteger(offset) || offset < 0) {{
    throw new ValidationError("{key}.offset must be a non-negative integer");
  }}
  if (!Number.isInteger(limit) || limit < 1 || limit > 200) {{
    throw new ValidationError("{key}.limit must be between 1 and 200");
  }}
  return {{ offset, limit }};
}}
"""


def mapper_file(key, name, human):
    return f"""import {{ {name}, {name}Status, summarize{name} }} from "./model";
import {{ {name}Page }} from "./repository";
import {{ {name}Summary }} from "./service";

export interface {name}Payload {{
  id: string;
  status: string;
  amount: string;
  reference: string;
  labels: readonly string[];
  summary: string;
  updatedAt: string;
}}

export interface {name}PagePayload {{
  items: readonly {name}Payload[];
  total: number;
  offset: number;
}}

export interface {name}SummaryPayload {{
  total: number;
  outstanding: number;
  amount: string;
  byStatus: Record<{name}Status, number>;
}}

function money(amountCents: number): string {{
  return (amountCents / 100).toFixed(2);
}}

/** Wire representation of a {human}; tenant identity never leaves the service. */
export function to{name}Payload(value: {name}): {name}Payload {{
  return {{
    id: value.id,
    status: value.status,
    amount: money(value.amountCents),
    reference: value.reference,
    labels: value.labels,
    summary: summarize{name}(value),
    updatedAt: value.updatedAt,
  }};
}}

export function to{name}Payloads(values: readonly {name}[]): {name}Payload[] {{
  return values.map(to{name}Payload);
}}

export function to{name}PagePayload(page: {name}Page): {name}PagePayload {{
  return {{ items: to{name}Payloads(page.items), total: page.total, offset: page.offset }};
}}

export function to{name}SummaryPayload(summary: {name}Summary): {name}SummaryPayload {{
  return {{
    total: summary.total,
    outstanding: summary.outstanding,
    amount: money(summary.amountCents),
    byStatus: summary.byStatus,
  }};
}}

export function totalAmountCents(values: readonly {name}[]): number {{
  return values.reduce((sum, value) => sum + value.amountCents, 0);
}}

export function to{name}CsvRow(value: {name}): string {{
  return [value.id, value.status, money(value.amountCents), value.reference].join(",");
}}
"""


def policy_file(key, name, human):
    return f"""import {{ ForbiddenError }} from "../../infra/errors";
import {{ hasPermission }} from "../../infra/permissions";

export type {name}Action = "read" | "write" | "administer";

const required: Record<{name}Action, readonly string[]> = {{
  read: ["{key}:read"],
  write: ["{key}:read", "{key}:write"],
  administer: ["{key}:read", "{key}:write", "{key}:admin"],
}};

/** Tenant-scoped authorization for {human} operations. */
export function assert{name}Access(tenantId: string, action: {name}Action): void {{
  if (tenantId.trim().length === 0) {{
    throw new ForbiddenError("{key} access requires a tenant header");
  }}
  for (const permission of required[action]) {{
    if (!hasPermission(tenantId, permission)) {{
      throw new ForbiddenError(`{key} ${{action}} is not granted for tenant ${{tenantId}}`);
    }}
  }}
}}

export function may{name}(tenantId: string, action: {name}Action): boolean {{
  try {{
    assert{name}Access(tenantId, action);
    return true;
  }} catch {{
    return false;
  }}
}}

export function grantedActions(tenantId: string): {name}Action[] {{
  const actions: {name}Action[] = ["read", "write", "administer"];
  return actions.filter((action) => may{name}(tenantId, action));
}}

export function assertSameTenant(tenantId: string, resourceTenantId: string): void {{
  if (tenantId !== resourceTenantId) {{
    throw new ForbiddenError("{key} belongs to a different tenant");
  }}
}}
"""


LAYER_BUILDERS = {"model": model_file, "repository": repository_file, "service": service_file,
                  "handler": handler_file, "validator": validator_file, "mapper": mapper_file,
                  "policy": policy_file}

INFRA = {
    "time.ts": """/** Deterministic timestamp helpers shared by every domain model. */
export function isoTimestamp(at: Date = new Date()): string {
  return at.toISOString();
}

export function durationMillis(from: string, to: string): number {
  return Date.parse(to) - Date.parse(from);
}

export function isBefore(left: string, right: string): boolean {
  return Date.parse(left) < Date.parse(right);
}
""",
    "errors.ts": """/** Typed failures mapped to HTTP status by the error middleware. */
export class ValidationError extends Error {}
export class NotFoundError extends Error {}
export class ForbiddenError extends Error {}
export class IllegalTransitionError extends Error {}
export class ConflictError extends Error {}

export function statusFor(error: unknown): number {
  if (error instanceof ValidationError) return 400;
  if (error instanceof ForbiddenError) return 403;
  if (error instanceof NotFoundError) return 404;
  if (error instanceof ConflictError) return 409;
  if (error instanceof IllegalTransitionError) return 422;
  return 500;
}
""",
    "audit.ts": """import { isoTimestamp } from "./time";

export interface AuditRecord {
  readonly at: string;
  readonly action: string;
  readonly detail: Record<string, unknown>;
}

const records: AuditRecord[] = [];

/** Append-only audit trail; callers never mutate the returned view. */
export function auditEvent(action: string, detail: Record<string, unknown>): void {
  records.push({ at: isoTimestamp(), action, detail });
}

export function auditTrail(): readonly AuditRecord[] {
  return records;
}
""",
    "permissions.ts": """const grants = new Map<string, Set<string>>();

/** Grants are seeded at boot; an unknown tenant has no permissions at all. */
export function grantPermission(tenantId: string, permission: string): void {
  const set = grants.get(tenantId) ?? new Set<string>();
  set.add(permission);
  grants.set(tenantId, set);
}

export function hasPermission(tenantId: string, permission: string): boolean {
  return grants.get(tenantId)?.has(permission) ?? false;
}

export function revokeTenant(tenantId: string): void {
  grants.delete(tenantId);
}
""",
}


def build():
    if os.path.exists(TARGET):
        shutil.rmtree(TARGET)
    files = {}
    for key, name, human in DOMAINS:
        for layer in LAYERS:
            files[f"src/domains/{key}/{layer}.ts"] = LAYER_BUILDERS[layer](key, name, human)
    for filename, body in INFRA.items():
        files[f"src/infra/{filename}"] = body
    # 其余基础设施: 每个域一个注册器过于重复, 改为按主题拆分的运行时装配文件。
    for index, topic in enumerate([
        "server", "router", "config", "logging", "metrics", "health", "readiness", "shutdown",
        "tracing", "ratelimit", "cors", "compression", "cache", "queue", "scheduler", "migrator",
        "seeder", "featureflags", "serialization", "clock", "random", "hashing", "encoding", "paging",
        "sorting", "filtering",
    ], start=1):
        files[f"src/runtime/{topic}.ts"] = runtime_file(topic, index)
    files["README.md"] = readme()
    files["package.json"] = package_json()
    files["tsconfig.json"] = tsconfig()

    for relative, body in files.items():
        path = os.path.join(TARGET, relative)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(body)
    return files


def runtime_file(topic, index):
    title = topic.capitalize()
    return f"""import {{ auditEvent }} from "../infra/audit";
import {{ isoTimestamp }} from "../infra/time";

export interface {title}Options {{
  readonly enabled: boolean;
  readonly label: string;
  readonly budgetMillis: number;
}}

const defaults: {title}Options = {{ enabled: true, label: "{topic}", budgetMillis: {index * 25} }};

/** Runtime {topic} concern, wired once during boot and never per request. */
export function configure{title}(overrides: Partial<{title}Options> = {{}}): {title}Options {{
  const options = {{ ...defaults, ...overrides }};
  if (options.budgetMillis <= 0) {{
    throw new Error("{topic} budget must be positive");
  }}
  auditEvent("runtime.{topic}.configured", {{ label: options.label, at: isoTimestamp() }});
  return options;
}}

export function describe{title}(options: {title}Options): string {{
  return `${{options.label}}: ${{options.enabled ? "on" : "off"}} (${{options.budgetMillis}}ms)`;
}}

export function within{title}Budget(options: {title}Options, elapsedMillis: number): boolean {{
  return options.enabled && elapsedMillis <= options.budgetMillis;
}}
"""


def readme():
    return """# scale-shop

Frozen large fixture for the AOCI black-box lifecycle harness. It exists so the
multi-batch authoring path and the relation-closure replan path can be exercised
at the real machine batch limit, which the small fixtures cannot reach.

The service is a layered TypeScript order platform: every business domain under
`src/domains/<domain>/` carries the same seven layers (model, repository,
service, handler, validator, mapper, policy), and the layering is the natural
relation shape the harness uses when it authors `R:` fields.

Generated once by `scripts/blackbox/generate_repo_c.py` and frozen. Regenerating
changes the fixture identity and the scale scenario expectations with it.
"""


def package_json():
    return """{
  "name": "scale-shop",
  "version": "1.0.0",
  "private": true,
  "description": "Frozen large fixture for AOCI scale scenarios",
  "type": "module",
  "dependencies": {
    "express": "4.19.2"
  },
  "devDependencies": {
    "typescript": "5.4.5",
    "@types/express": "4.17.21"
  }
}
"""


def tsconfig():
    return """{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "declaration": true,
    "outDir": "dist",
    "rootDir": "src"
  },
  "include": ["src"]
}
"""


def survey():
    total, lines, digest = 0, 0, hashlib.sha256()
    for base, _, names in sorted(os.walk(TARGET)):
        for name in sorted(names):
            path = os.path.join(base, name)
            data = open(path, "rb").read()
            total += 1
            lines += data.count(b"\n")
            digest.update(os.path.relpath(path, TARGET).encode())
            digest.update(hashlib.sha256(data).digest())
    return total, lines, digest.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="只统计现有夹具,不重建")
    args = parser.parse_args()
    if not args.check:
        build()
    if not os.path.exists(TARGET):
        print("fixtures/repo-c 不存在", file=sys.stderr)
        return 1
    total, lines, digest = survey()
    print(f"files={total} lines={lines} identity={digest[:16]}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
