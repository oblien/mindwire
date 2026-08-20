import defaultMdxComponents from "fumadocs-ui/mdx";
import { Tab, Tabs } from "fumadocs-ui/components/tabs";
import type { MDXComponents } from "mdx/types";
import { APIPage } from "@/components/api-page";
import { ApiTabs } from "@/components/api-tabs";

// Components available to every MDX page. ApiTabs (over Tabs/Tab) powers the TypeScript/Go/REST
// tri-surface snippets with a persisted language choice; APIPage is the interactive REST renderer — the
// generated OpenAPI pages read it (and its legacy alias OpenAPIPage) off `props.components`.
export function getMDXComponents(components?: MDXComponents): MDXComponents {
  return {
    ...defaultMdxComponents,
    Tabs,
    Tab,
    ApiTabs,
    APIPage,
    OpenAPIPage: APIPage,
    ...components,
  };
}
