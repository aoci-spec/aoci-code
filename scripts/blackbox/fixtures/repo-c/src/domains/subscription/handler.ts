import { NextFunction, Request, Response } from "express";
import { SubscriptionService } from "./service";
import {
  parseSubscriptionCreate,
  parseSubscriptionLabel,
  parseSubscriptionPage,
  parseSubscriptionTransition,
} from "./validator";
import { toSubscriptionPagePayload, toSubscriptionPayload, toSubscriptionSummaryPayload } from "./mapper";
import { assertSubscriptionAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for recurring subscription resources. */
export function makeSubscriptionHandlers(service: SubscriptionService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSubscriptionAccess(tenantId, "write");
        const input = parseSubscriptionCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toSubscriptionPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSubscriptionAccess(tenantId, "read");
        response.json(toSubscriptionPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSubscriptionAccess(tenantId, "write");
        const input = parseSubscriptionTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toSubscriptionPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSubscriptionAccess(tenantId, "write");
        const input = parseSubscriptionLabel(request.body);
        response.json(toSubscriptionPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSubscriptionAccess(tenantId, "read");
        const page = parseSubscriptionPage(request.query);
        response.json(toSubscriptionPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSubscriptionAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toSubscriptionPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSubscriptionAccess(tenantId, "read");
        response.json(toSubscriptionSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertSubscriptionAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
