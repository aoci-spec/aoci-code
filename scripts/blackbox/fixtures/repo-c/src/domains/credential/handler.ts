import { NextFunction, Request, Response } from "express";
import { CredentialService } from "./service";
import {
  parseCredentialCreate,
  parseCredentialLabel,
  parseCredentialPage,
  parseCredentialTransition,
} from "./validator";
import { toCredentialPagePayload, toCredentialPayload, toCredentialSummaryPayload } from "./mapper";
import { assertCredentialAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for stored credential resources. */
export function makeCredentialHandlers(service: CredentialService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCredentialAccess(tenantId, "write");
        const input = parseCredentialCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toCredentialPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCredentialAccess(tenantId, "read");
        response.json(toCredentialPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCredentialAccess(tenantId, "write");
        const input = parseCredentialTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toCredentialPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCredentialAccess(tenantId, "write");
        const input = parseCredentialLabel(request.body);
        response.json(toCredentialPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCredentialAccess(tenantId, "read");
        const page = parseCredentialPage(request.query);
        response.json(toCredentialPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCredentialAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toCredentialPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCredentialAccess(tenantId, "read");
        response.json(toCredentialSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertCredentialAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
