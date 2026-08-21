import { Tabs } from "fumadocs-ui/components/tabs";
import type { ReactNode } from "react";

// The tri-surface snippet switcher used across the guides: TypeScript / Go / REST, always in that order.
// A shared groupId + persist means picking "Go" on one page keeps every other guide on Go too, so a
// reader following the docs in their language never re-selects. Author pages as:
//   <ApiTabs>
//   <Tab value="TypeScript"> …ts… </Tab>
//   <Tab value="Go"> …go… </Tab>
//   <Tab value="REST"> …bash… </Tab>
//   </ApiTabs>
export function ApiTabs({ children }: { children: ReactNode }) {
  return (
    <Tabs items={["TypeScript", "Go", "REST"]} groupId="mindwire-lang" persist>
      {children}
    </Tabs>
  );
}
