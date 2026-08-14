import { NextFunction, Request, Response } from "express";
import { InventoryService } from "./service";
import {
  parseInventoryCreate,
  parseInventoryLabel,
  parseInventoryPage,
  parseInventoryTransition,
} from "./validator";
import { toInventoryPagePayload, toInventoryPayload, toInventorySummaryPayload } from "./mapper";
import { assertInventoryAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for stock position resources. */
export function makeInventoryHandlers(service: InventoryService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInventoryAccess(tenantId, "write");
        const input = parseInventoryCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toInventoryPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInventoryAccess(tenantId, "read");
        response.json(toInventoryPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInventoryAccess(tenantId, "write");
        const input = parseInventoryTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toInventoryPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInventoryAccess(tenantId, "write");
        const input = parseInventoryLabel(request.body);
        response.json(toInventoryPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInventoryAccess(tenantId, "read");
        const page = parseInventoryPage(request.query);
        response.json(toInventoryPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInventoryAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toInventoryPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInventoryAccess(tenantId, "read");
        response.json(toInventorySummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertInventoryAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
