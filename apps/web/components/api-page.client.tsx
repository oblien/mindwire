"use client";
import { createOpenAPIPage } from "fumadocs-openapi/ui";

// createOpenAPIPage() must be *called* on the client (it's a client factory). Building the component here,
// in a client module, lets the server wrapper (api-page.tsx) import it as a client reference and render it
// with serializable props. Shiki + playground default options are used as-is; the fd-* / zero-radius theme
// carries through from globals.css.
export const ClientAPIPage = createOpenAPIPage();
