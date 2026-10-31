"use client";

import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

// Keep the marketing footer off the docs routes, which own their layout.
// Children stay server-rendered (passed in as a prop), this wrapper only gates them.
export default function HideOnDocs({ children, hideOnHome = false }: { children: ReactNode; hideOnHome?: boolean }) {
  const pathname = usePathname();
  if (pathname?.startsWith("/docs") || pathname?.startsWith("/design-lab") || (hideOnHome && pathname === "/")) return null;
  return <>{children}</>;
}
