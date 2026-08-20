import { source } from "@/lib/source";
import { createFromSource } from "fumadocs-core/search/server";

// Server-side Orama search over the docs tree (replaces the static Pagefind index).
export const { GET } = createFromSource(source);
