import { NextFunction, Request, Response } from "express";
import { AuditService } from "./service";
import {
  parseAuditCreate,
  parseAuditLabel,
  parseAuditPage,
  parseAuditTransition,
} from "./validator";
import { toAuditPagePayload, toAuditPayload, toAuditSummaryPayload } from "./mapper";
import { assertAuditAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for audit trail record resources. */
export function makeAuditHandlers(service: AuditService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAuditAccess(tenantId, "write");
        const input = parseAuditCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toAuditPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAuditAccess(tenantId, "read");
        response.json(toAuditPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAuditAccess(tenantId, "write");
        const input = parseAuditTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toAuditPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAuditAccess(tenantId, "write");
        const input = parseAuditLabel(request.body);
        response.json(toAuditPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAuditAccess(tenantId, "read");
        const page = parseAuditPage(request.query);
        response.json(toAuditPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAuditAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toAuditPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAuditAccess(tenantId, "read");
        response.json(toAuditSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAuditAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
