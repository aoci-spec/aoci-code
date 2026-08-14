import { NextFunction, Request, Response } from "express";
import { ShipmentService } from "./service";
import {
  parseShipmentCreate,
  parseShipmentLabel,
  parseShipmentPage,
  parseShipmentTransition,
} from "./validator";
import { toShipmentPagePayload, toShipmentPayload, toShipmentSummaryPayload } from "./mapper";
import { assertShipmentAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for outbound shipment resources. */
export function makeShipmentHandlers(service: ShipmentService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertShipmentAccess(tenantId, "write");
        const input = parseShipmentCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toShipmentPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertShipmentAccess(tenantId, "read");
        response.json(toShipmentPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertShipmentAccess(tenantId, "write");
        const input = parseShipmentTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toShipmentPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertShipmentAccess(tenantId, "write");
        const input = parseShipmentLabel(request.body);
        response.json(toShipmentPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertShipmentAccess(tenantId, "read");
        const page = parseShipmentPage(request.query);
        response.json(toShipmentPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertShipmentAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toShipmentPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertShipmentAccess(tenantId, "read");
        response.json(toShipmentSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertShipmentAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
