import { openapi } from "@/lib/openapi";
import { ClientAPIPage } from "./api-page.client";
import type { OperationItem, WebhookItem } from "fumadocs-openapi/ui";

// The interactive REST renderer. Generated pages under content/docs/reference/rest render
// `<APIPage document="../../daemon/openapi.json" operations={[...]} />` and pull this component from
// mdx-components. The client renderer (ClientAPIPage) needs the *resolved* schema, not a path, so this
// server wrapper loads + bundles the document — openapi.getSchema is cached and keyed by the same input
// string createOpenAPI was constructed with — and hands it down as the serializable `payload`.
type APIPageProps = {
  document: string;
  operations?: OperationItem[];
  webhooks?: WebhookItem[];
  showTitle?: boolean;
  showDescription?: boolean;
};

export async function APIPage({ document, ...rest }: APIPageProps) {
  const { bundled } = await openapi.getSchema(document);
  return <ClientAPIPage payload={{ bundled }} {...rest} />;
}
