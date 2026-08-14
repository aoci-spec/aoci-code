import { NextFunction, Request, Response } from "express";
import { VariantService } from "./service";
import {
  parseVariantCreate,
  parseVariantLabel,
  parseVariantPage,
  parseVariantTransition,
} from "./validator";
import { toVariantPagePayload, toVariantPayload, toVariantSummaryPayload } from "./mapper";
import { assertVariantAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for product variant resources. */
export function makeVariantHandlers(service: VariantService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertVariantAccess(tenantId, "write");
        const input = parseVariantCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toVariantPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertVariantAccess(tenantId, "read");
        response.json(toVariantPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertVariantAccess(tenantId, "write");
        const input = parseVariantTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toVariantPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertVariantAccess(tenantId, "write");
        const input = parseVariantLabel(request.body);
        response.json(toVariantPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertVariantAccess(tenantId, "read");
        const page = parseVariantPage(request.query);
        response.json(toVariantPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertVariantAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toVariantPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertVariantAccess(tenantId, "read");
        response.json(toVariantSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertVariantAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
