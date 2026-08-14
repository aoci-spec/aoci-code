import { NextFunction, Request, Response } from "express";
import { ProductService } from "./service";
import {
  parseProductCreate,
  parseProductLabel,
  parseProductPage,
  parseProductTransition,
} from "./validator";
import { toProductPagePayload, toProductPayload, toProductSummaryPayload } from "./mapper";
import { assertProductAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for sellable product resources. */
export function makeProductHandlers(service: ProductService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertProductAccess(tenantId, "write");
        const input = parseProductCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toProductPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertProductAccess(tenantId, "read");
        response.json(toProductPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertProductAccess(tenantId, "write");
        const input = parseProductTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toProductPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertProductAccess(tenantId, "write");
        const input = parseProductLabel(request.body);
        response.json(toProductPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertProductAccess(tenantId, "read");
        const page = parseProductPage(request.query);
        response.json(toProductPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertProductAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toProductPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertProductAccess(tenantId, "read");
        response.json(toProductSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertProductAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
