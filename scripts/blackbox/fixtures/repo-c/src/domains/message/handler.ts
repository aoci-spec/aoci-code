import { NextFunction, Request, Response } from "express";
import { MessageService } from "./service";
import {
  parseMessageCreate,
  parseMessageLabel,
  parseMessagePage,
  parseMessageTransition,
} from "./validator";
import { toMessagePagePayload, toMessagePayload, toMessageSummaryPayload } from "./mapper";
import { assertMessageAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for customer message resources. */
export function makeMessageHandlers(service: MessageService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMessageAccess(tenantId, "write");
        const input = parseMessageCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toMessagePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMessageAccess(tenantId, "read");
        response.json(toMessagePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMessageAccess(tenantId, "write");
        const input = parseMessageTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toMessagePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMessageAccess(tenantId, "write");
        const input = parseMessageLabel(request.body);
        response.json(toMessagePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMessageAccess(tenantId, "read");
        const page = parseMessagePage(request.query);
        response.json(toMessagePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMessageAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toMessagePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMessageAccess(tenantId, "read");
        response.json(toMessageSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertMessageAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
