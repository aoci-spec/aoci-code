import { NextFunction, Request, Response } from "express";
import { AddressService } from "./service";
import {
  parseAddressCreate,
  parseAddressLabel,
  parseAddressPage,
  parseAddressTransition,
} from "./validator";
import { toAddressPagePayload, toAddressPayload, toAddressSummaryPayload } from "./mapper";
import { assertAddressAccess } from "./policy";

function tenantOf(request: Request): string {
  return String(request.header("x-tenant") ?? "");
}

/** HTTP surface for postal address resources. */
export function makeAddressHandlers(service: AddressService) {
  return {
    create(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAddressAccess(tenantId, "write");
        const input = parseAddressCreate(request.body);
        const created = service.create(tenantId, input.id, input.reference, input.amountCents);
        response.status(201).json(toAddressPayload(created));
      } catch (error) {
        next(error);
      }
    },

    get(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAddressAccess(tenantId, "read");
        response.json(toAddressPayload(service.get(tenantId, String(request.params.id))));
      } catch (error) {
        next(error);
      }
    },

    transition(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAddressAccess(tenantId, "write");
        const input = parseAddressTransition(request.body);
        const updated = service.transition(tenantId, String(request.params.id), input.status);
        response.json(toAddressPayload(updated));
      } catch (error) {
        next(error);
      }
    },

    label(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAddressAccess(tenantId, "write");
        const input = parseAddressLabel(request.body);
        response.json(toAddressPayload(service.label(tenantId, String(request.params.id), input.label)));
      } catch (error) {
        next(error);
      }
    },

    list(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAddressAccess(tenantId, "read");
        const page = parseAddressPage(request.query);
        response.json(toAddressPagePayload(service.page(tenantId, page.offset, page.limit)));
      } catch (error) {
        next(error);
      }
    },

    outstanding(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAddressAccess(tenantId, "read");
        response.json(service.outstanding(tenantId).map(toAddressPayload));
      } catch (error) {
        next(error);
      }
    },

    summary(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAddressAccess(tenantId, "read");
        response.json(toAddressSummaryPayload(service.summary(tenantId)));
      } catch (error) {
        next(error);
      }
    },

    discard(request: Request, response: Response, next: NextFunction): void {
      try {
        const tenantId = tenantOf(request);
        assertAddressAccess(tenantId, "write");
        service.discard(tenantId, String(request.params.id));
        response.status(204).end();
      } catch (error) {
        next(error);
      }
    },
  };
}
