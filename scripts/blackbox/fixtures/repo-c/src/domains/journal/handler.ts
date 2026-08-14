import { NextFunction, Request, Response } from "express";
import { JournalService } from "./service";
import {
  parseJournalCreate,
  parseJournalLabel,
  parseJournalPage,
  parseJournalTransition,
} from "./validator";
import { toJournalPagePayload, toJournalPayload, toJournalSummaryPayload } from "./mapper";
import { assertJournalAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for journal batch resources. */
export function makeJournalHandlers(service: JournalService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJournalAccess(tenantId, "write");
        const input = parseJournalCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toJournalPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJournalAccess(tenantId, "read");
        response.json(toJournalPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJournalAccess(tenantId, "write");
        const input = parseJournalTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toJournalPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJournalAccess(tenantId, "write");
        const input = parseJournalLabel(request.body);
        response.json(toJournalPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJournalAccess(tenantId, "read");
        const page = parseJournalPage(request.query);
        response.json(toJournalPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJournalAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toJournalPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJournalAccess(tenantId, "read");
        response.json(toJournalSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertJournalAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
