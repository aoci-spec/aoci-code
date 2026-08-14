import { NextFunction, Request, Response } from "express";
import { QuotaService } from "./service";
import {
  parseQuotaCreate,
  parseQuotaLabel,
  parseQuotaPage,
  parseQuotaTransition,
} from "./validator";
import { toQuotaPagePayload, toQuotaPayload, toQuotaSummaryPayload } from "./mapper";
import { assertQuotaAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for consumption quota resources. */
export function makeQuotaHandlers(service: QuotaService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertQuotaAccess(tenantId, "write");
        const input = parseQuotaCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toQuotaPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertQuotaAccess(tenantId, "read");
        response.json(toQuotaPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertQuotaAccess(tenantId, "write");
        const input = parseQuotaTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toQuotaPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertQuotaAccess(tenantId, "write");
        const input = parseQuotaLabel(request.body);
        response.json(toQuotaPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertQuotaAccess(tenantId, "read");
        const page = parseQuotaPage(request.query);
        response.json(toQuotaPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertQuotaAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toQuotaPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertQuotaAccess(tenantId, "read");
        response.json(toQuotaSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertQuotaAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
