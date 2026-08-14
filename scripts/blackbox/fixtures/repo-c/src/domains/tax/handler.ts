import { NextFunction, Request, Response } from "express";
import { TaxService } from "./service";
import {
  parseTaxCreate,
  parseTaxLabel,
  parseTaxPage,
  parseTaxTransition,
} from "./validator";
import { toTaxPagePayload, toTaxPayload, toTaxSummaryPayload } from "./mapper";
import { assertTaxAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for tax determination resources. */
export function makeTaxHandlers(service: TaxService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaxAccess(tenantId, "write");
        const input = parseTaxCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toTaxPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaxAccess(tenantId, "read");
        response.json(toTaxPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaxAccess(tenantId, "write");
        const input = parseTaxTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toTaxPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaxAccess(tenantId, "write");
        const input = parseTaxLabel(request.body);
        response.json(toTaxPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaxAccess(tenantId, "read");
        const page = parseTaxPage(request.query);
        response.json(toTaxPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaxAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toTaxPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaxAccess(tenantId, "read");
        response.json(toTaxSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTaxAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
