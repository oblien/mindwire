import { Tabs } from "fumadocs-ui/components/tabs";
import type { ComponentProps } from "react";

export function DocsTabs({
  className = "",
  ...props
}: ComponentProps<typeof Tabs>) {
  return <Tabs {...props} className={`docs-tabs ${className}`} />;
}
