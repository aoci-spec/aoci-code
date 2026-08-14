import { NextFunction, Request, Response } from "express";
import { CarrierService } from "./service";
import {
  parseCarrierCreate,
  parseCarrierLabel,
  parseCarrierPage,
  parseCarrierTransition,
} from "./validator";
import { toCarrierPagePayload, toCarrierPayload, toCarrierSummaryPayload } from "./mapper";
import { assertCarrierAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for delivery carrier resources. */
export function makeCarrierHandlers(service: CarrierService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCarrierAccess(tenantId, "write");
        const input = parseCarrierCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toCarrierPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCarrierAccess(tenantId, "read");
        response.json(toCarrierPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCarrierAccess(tenantId, "write");
        const input = parseCarrierTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toCarrierPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCarrierAccess(tenantId, "write");
        const input = parseCarrierLabel(request.body);
        response.json(toCarrierPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCarrierAccess(tenantId, "read");
        const page = parseCarrierPage(request.query);
        response.json(toCarrierPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCarrierAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toCarrierPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCarrierAccess(tenantId, "read");
        response.json(toCarrierSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCarrierAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
