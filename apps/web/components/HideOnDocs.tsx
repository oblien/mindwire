"use client";

import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

// Renders its children everywhere except under /docs. Used to keep the marketing <Footer> off the docs
// routes — the Fumadocs sidebar is a fixed, full-height column, so the site footer beneath it reads as
// clutter. Children stay server-rendered (passed in as a prop), this wrapper only gates them.
export default function HideOnDocs({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  if (pathname?.startsWith("/docs")) return null;
  return <>{children}</>;
}
