import { NextFunction, Request, Response } from "express";
import { SettlementService } from "./service";
import {
  parseSettlementCreate,
  parseSettlementLabel,
  parseSettlementPage,
  parseSettlementTransition,
} from "./validator";
import { toSettlementPagePayload, toSettlementPayload, toSettlementSummaryPayload } from "./mapper";
import { assertSettlementAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for settlement run resources. */
export function makeSettlementHandlers(service: SettlementService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettlementAccess(tenantId, "write");
        const input = parseSettlementCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toSettlementPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettlementAccess(tenantId, "read");
        response.json(toSettlementPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettlementAccess(tenantId, "write");
        const input = parseSettlementTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toSettlementPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettlementAccess(tenantId, "write");
        const input = parseSettlementLabel(request.body);
        response.json(toSettlementPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettlementAccess(tenantId, "read");
        const page = parseSettlementPage(request.query);
        response.json(toSettlementPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettlementAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toSettlementPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettlementAccess(tenantId, "read");
        response.json(toSettlementSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSettlementAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
