"use client";

import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import styles from "./site.module.css";

export default function SiteTheme({ children }: { children: ReactNode }) {
  const path = usePathname();
  if (path === "/docs" || path?.startsWith("/docs/")) return children;
  return (
    <div className={styles.theme} data-site-theme="paper">
      {children}
    </div>
  );
}
