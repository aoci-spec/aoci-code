import { NextFunction, Request, Response } from "express";
import { WarrantyService } from "./service";
import {
  parseWarrantyCreate,
  parseWarrantyLabel,
  parseWarrantyPage,
  parseWarrantyTransition,
} from "./validator";
import { toWarrantyPagePayload, toWarrantyPayload, toWarrantySummaryPayload } from "./mapper";
import { assertWarrantyAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for warranty claim resources. */
export function makeWarrantyHandlers(service: WarrantyService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarrantyAccess(tenantId, "write");
        const input = parseWarrantyCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toWarrantyPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarrantyAccess(tenantId, "read");
        response.json(toWarrantyPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarrantyAccess(tenantId, "write");
        const input = parseWarrantyTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toWarrantyPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarrantyAccess(tenantId, "write");
        const input = parseWarrantyLabel(request.body);
        response.json(toWarrantyPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarrantyAccess(tenantId, "read");
        const page = parseWarrantyPage(request.query);
        response.json(toWarrantyPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarrantyAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toWarrantyPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarrantyAccess(tenantId, "read");
        response.json(toWarrantySummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarrantyAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
