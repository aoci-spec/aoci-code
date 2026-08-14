import { NextFunction, Request, Response } from "express";
import { TemplateService } from "./service";
import {
  parseTemplateCreate,
  parseTemplateLabel,
  parseTemplatePage,
  parseTemplateTransition,
} from "./validator";
import { toTemplatePagePayload, toTemplatePayload, toTemplateSummaryPayload } from "./mapper";
import { assertTemplateAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for message template resources. */
export function makeTemplateHandlers(service: TemplateService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTemplateAccess(tenantId, "write");
        const input = parseTemplateCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toTemplatePayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTemplateAccess(tenantId, "read");
        response.json(toTemplatePayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTemplateAccess(tenantId, "write");
        const input = parseTemplateTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toTemplatePayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTemplateAccess(tenantId, "write");
        const input = parseTemplateLabel(request.body);
        response.json(toTemplatePayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTemplateAccess(tenantId, "read");
        const page = parseTemplatePage(request.query);
        response.json(toTemplatePagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTemplateAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toTemplatePayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTemplateAccess(tenantId, "read");
        response.json(toTemplateSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertTemplateAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
