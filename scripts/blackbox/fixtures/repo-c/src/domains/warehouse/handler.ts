import { NextFunction, Request, Response } from "express";
import { WarehouseService } from "./service";
import {
  parseWarehouseCreate,
  parseWarehouseLabel,
  parseWarehousePage,
  parseWarehouseTransition,
} from "./validator";
import { toWarehousePagePayload, toWarehousePayload, toWarehouseSummaryPayload } from "./mapper";
import { assertWarehouseAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for storage facility resources. */
export function makeWarehouseHandlers(service: WarehouseService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarehouseAccess(tenantId, "write");
        const input = parseWarehouseCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toWarehousePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarehouseAccess(tenantId, "read");
        response.json(toWarehousePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarehouseAccess(tenantId, "write");
        const input = parseWarehouseTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toWarehousePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarehouseAccess(tenantId, "write");
        const input = parseWarehouseLabel(request.body);
        response.json(toWarehousePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarehouseAccess(tenantId, "read");
        const page = parseWarehousePage(request.query);
        response.json(toWarehousePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarehouseAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toWarehousePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarehouseAccess(tenantId, "read");
        response.json(toWarehouseSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWarehouseAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
