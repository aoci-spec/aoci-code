import { NextFunction, Request, Response } from "express";
import { DocumentService } from "./service";
import {
  parseDocumentCreate,
  parseDocumentLabel,
  parseDocumentPage,
  parseDocumentTransition,
} from "./validator";
import { toDocumentPagePayload, toDocumentPayload, toDocumentSummaryPayload } from "./mapper";
import { assertDocumentAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for stored document resources. */
export function makeDocumentHandlers(service: DocumentService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDocumentAccess(tenantId, "write");
        const input = parseDocumentCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toDocumentPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDocumentAccess(tenantId, "read");
        response.json(toDocumentPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDocumentAccess(tenantId, "write");
        const input = parseDocumentTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toDocumentPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDocumentAccess(tenantId, "write");
        const input = parseDocumentLabel(request.body);
        response.json(toDocumentPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDocumentAccess(tenantId, "read");
        const page = parseDocumentPage(request.query);
        response.json(toDocumentPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDocumentAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toDocumentPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDocumentAccess(tenantId, "read");
        response.json(toDocumentSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertDocumentAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
