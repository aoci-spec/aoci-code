import { NextFunction, Request, Response } from "express";
import { DisputeService } from "./service";
import {
  parseDisputeCreate,
  parseDisputeLabel,
  parseDisputePage,
  parseDisputeTransition,
} from "./validator";
import { toDisputePagePayload, toDisputePayload, toDisputeSummaryPayload } from "./mapper";
import { assertDisputeAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for payment dispute resources. */
export function makeDisputeHandlers(service: DisputeService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDisputeAccess(tenantId, "write");
        const input = parseDisputeCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toDisputePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDisputeAccess(tenantId, "read");
        response.json(toDisputePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDisputeAccess(tenantId, "write");
        const input = parseDisputeTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toDisputePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDisputeAccess(tenantId, "write");
        const input = parseDisputeLabel(request.body);
        response.json(toDisputePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDisputeAccess(tenantId, "read");
        const page = parseDisputePage(request.query);
        response.json(toDisputePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDisputeAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toDisputePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDisputeAccess(tenantId, "read");
        response.json(toDisputeSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDisputeAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
