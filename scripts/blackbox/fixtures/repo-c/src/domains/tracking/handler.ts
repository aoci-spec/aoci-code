import { NextFunction, Request, Response } from "express";
import { TrackingService } from "./service";
import {
  parseTrackingCreate,
  parseTrackingLabel,
  parseTrackingPage,
  parseTrackingTransition,
} from "./validator";
import { toTrackingPagePayload, toTrackingPayload, toTrackingSummaryPayload } from "./mapper";
import { assertTrackingAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for tracking event resources. */
export function makeTrackingHandlers(service: TrackingService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTrackingAccess(tenantId, "write");
        const input = parseTrackingCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toTrackingPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTrackingAccess(tenantId, "read");
        response.json(toTrackingPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTrackingAccess(tenantId, "write");
        const input = parseTrackingTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toTrackingPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTrackingAccess(tenantId, "write");
        const input = parseTrackingLabel(request.body);
        response.json(toTrackingPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTrackingAccess(tenantId, "read");
        const page = parseTrackingPage(request.query);
        response.json(toTrackingPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTrackingAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toTrackingPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTrackingAccess(tenantId, "read");
        response.json(toTrackingSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTrackingAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
