import { NextFunction, Request, Response } from "express";
import { NotificationService } from "./service";
import {
  parseNotificationCreate,
  parseNotificationLabel,
  parseNotificationPage,
  parseNotificationTransition,
} from "./validator";
import { toNotificationPagePayload, toNotificationPayload, toNotificationSummaryPayload } from "./mapper";
import { assertNotificationAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for outbound notification resources. */
export function makeNotificationHandlers(service: NotificationService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertNotificationAccess(tenantId, "write");
        const input = parseNotificationCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toNotificationPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertNotificationAccess(tenantId, "read");
        response.json(toNotificationPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertNotificationAccess(tenantId, "write");
        const input = parseNotificationTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toNotificationPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertNotificationAccess(tenantId, "write");
        const input = parseNotificationLabel(request.body);
        response.json(toNotificationPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertNotificationAccess(tenantId, "read");
        const page = parseNotificationPage(request.query);
        response.json(toNotificationPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertNotificationAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toNotificationPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertNotificationAccess(tenantId, "read");
        response.json(toNotificationSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertNotificationAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
