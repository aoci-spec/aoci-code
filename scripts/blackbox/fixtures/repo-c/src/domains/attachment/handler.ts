import { NextFunction, Request, Response } from "express";
import { AttachmentService } from "./service";
import {
  parseAttachmentCreate,
  parseAttachmentLabel,
  parseAttachmentPage,
  parseAttachmentTransition,
} from "./validator";
import { toAttachmentPagePayload, toAttachmentPayload, toAttachmentSummaryPayload } from "./mapper";
import { assertAttachmentAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for file attachment resources. */
export function makeAttachmentHandlers(service: AttachmentService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAttachmentAccess(tenantId, "write");
        const input = parseAttachmentCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toAttachmentPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAttachmentAccess(tenantId, "read");
        response.json(toAttachmentPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAttachmentAccess(tenantId, "write");
        const input = parseAttachmentTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toAttachmentPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAttachmentAccess(tenantId, "write");
        const input = parseAttachmentLabel(request.body);
        response.json(toAttachmentPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAttachmentAccess(tenantId, "read");
        const page = parseAttachmentPage(request.query);
        response.json(toAttachmentPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAttachmentAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toAttachmentPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAttachmentAccess(tenantId, "read");
        response.json(toAttachmentSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAttachmentAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
