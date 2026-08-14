import { NextFunction, Request, Response } from "express";
import { CatalogService } from "./service";
import {
  parseCatalogCreate,
  parseCatalogLabel,
  parseCatalogPage,
  parseCatalogTransition,
} from "./validator";
import { toCatalogPagePayload, toCatalogPayload, toCatalogSummaryPayload } from "./mapper";
import { assertCatalogAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for product catalog resources. */
export function makeCatalogHandlers(service: CatalogService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCatalogAccess(tenantId, "write");
        const input = parseCatalogCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toCatalogPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCatalogAccess(tenantId, "read");
        response.json(toCatalogPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCatalogAccess(tenantId, "write");
        const input = parseCatalogTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toCatalogPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCatalogAccess(tenantId, "write");
        const input = parseCatalogLabel(request.body);
        response.json(toCatalogPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCatalogAccess(tenantId, "read");
        const page = parseCatalogPage(request.query);
        response.json(toCatalogPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCatalogAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toCatalogPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCatalogAccess(tenantId, "read");
        response.json(toCatalogSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCatalogAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
