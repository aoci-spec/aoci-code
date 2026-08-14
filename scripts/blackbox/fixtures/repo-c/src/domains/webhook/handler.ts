import { NextFunction, Request, Response } from "express";
import { WebhookService } from "./service";
import {
  parseWebhookCreate,
  parseWebhookLabel,
  parseWebhookPage,
  parseWebhookTransition,
} from "./validator";
import { toWebhookPagePayload, toWebhookPayload, toWebhookSummaryPayload } from "./mapper";
import { assertWebhookAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for outbound webhook resources. */
export function makeWebhookHandlers(service: WebhookService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWebhookAccess(tenantId, "write");
        const input = parseWebhookCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toWebhookPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWebhookAccess(tenantId, "read");
        response.json(toWebhookPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWebhookAccess(tenantId, "write");
        const input = parseWebhookTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toWebhookPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWebhookAccess(tenantId, "write");
        const input = parseWebhookLabel(request.body);
        response.json(toWebhookPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWebhookAccess(tenantId, "read");
        const page = parseWebhookPage(request.query);
        response.json(toWebhookPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWebhookAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toWebhookPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWebhookAccess(tenantId, "read");
        response.json(toWebhookSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertWebhookAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
