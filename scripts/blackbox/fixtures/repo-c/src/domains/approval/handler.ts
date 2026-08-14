import { NextFunction, Request, Response } from "express";
import { ApprovalService } from "./service";
import {
  parseApprovalCreate,
  parseApprovalLabel,
  parseApprovalPage,
  parseApprovalTransition,
} from "./validator";
import { toApprovalPagePayload, toApprovalPayload, toApprovalSummaryPayload } from "./mapper";
import { assertApprovalAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for approval decision resources. */
export function makeApprovalHandlers(service: ApprovalService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertApprovalAccess(tenantId, "write");
        const input = parseApprovalCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toApprovalPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertApprovalAccess(tenantId, "read");
        response.json(toApprovalPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertApprovalAccess(tenantId, "write");
        const input = parseApprovalTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toApprovalPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertApprovalAccess(tenantId, "write");
        const input = parseApprovalLabel(request.body);
        response.json(toApprovalPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertApprovalAccess(tenantId, "read");
        const page = parseApprovalPage(request.query);
        response.json(toApprovalPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertApprovalAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toApprovalPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertApprovalAccess(tenantId, "read");
        response.json(toApprovalSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertApprovalAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
