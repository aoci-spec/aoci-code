import express from "express";
import { listItems } from "./service.js";

export const router = express.Router();
router.get("/items", async (_request, response) => response.json(await listItems()));
