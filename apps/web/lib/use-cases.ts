export type UseCaseArtKind =
  "calendar" | "notes" | "plan" | "map" | "chart" | "folders" | "book" | "app";

export type ProductDemoKind =
  "research" | "builder" | "support" | "analytics" | "learning" | "commerce";
export type UseCase = {
  slug: string;
  title: string;
  headline: string;
  category: string;
  brand: string;
  platform: "Desktop" | "Cloud";
  kind: ProductDemoKind;
  summary: string;
  opportunity: string;
  request: string;
  reply: string;
  context: string[];
  tools: { name: string; purpose: string }[];
  outcome: string;
  integration: string;
};

/** Fictional product blueprints, not customer stories or live agent sessions. */
export const useCases: UseCase[] = [
  {
    slug: "desktop-research",
    title: "A research workspace",
    headline: "Your research app. Now it can do the research.",
    category: "Research & knowledge",
    brand: "Folio",
    platform: "Desktop",
    kind: "research",
    summary:
      "Discover the agents already on a customer’s computer. Turn their project files into a sourced, editable report inside your app.",
    opportunity:
      "Your customers already collect interviews, notes, and documents in your app. Give them a way to work through that material, find the evidence, and build something from it.",
    request:
      "Compare the customer interviews. What’s blocking onboarding? Build a findings report and show me the evidence.",
    reply:
      "I found three recurring problems across six interviews. The report links each finding to its source, so you can check the evidence before sharing it.",
    context: [
      "6 customer interviews",
      "Onboarding research folder",
      "Your report template",
    ],
    tools: [
      {
        name: "search_documents",
        purpose: "Find relevant passages in the project.",
      },
      {
        name: "read_document",
        purpose: "Read the original interview and keep its source reference.",
      },
      {
        name: "save_report",
        purpose: "Create an editable report in your workspace.",
      },
    ],
    outcome: "A report your customers can inspect, refine, and share.",
    integration:
      "Bundle the runtime with your desktop app, discover supported native agents, and verify the customer’s existing connection. Give the session access to the selected project and your document tools. Your app renders the report and its source links.",
  },
  {
    slug: "cloud-site-builder",
    title: "A website builder",
    headline: "Your builder. Their idea. A working website.",
    category: "Website & app builders",
    brand: "Forma",
    platform: "Cloud",
    kind: "builder",
    summary:
      "Let customers connect their ChatGPT account, describe a website, and keep building in your editor—with a real agent working behind it.",
    opportunity:
      "You already have an editor, hosting, and a reason for customers to build. Add the agent that turns a brief into a working site, revises the code, and checks the result in the same workspace.",
    request:
      "Build a website for our coastal retreat. Use the brief and photos. Include the itinerary, a booking section, and a mobile layout.",
    reply:
      "The retreat site is ready to preview. I built the page, added the itinerary and booking section, and checked the responsive layout. Publishing is up to you.",
    context: [
      "retreat-brief.md",
      "Product photos & brand styles",
      "Your page components",
    ],
    tools: [
      {
        name: "read_project",
        purpose: "Use the brief, assets, and existing page components.",
      },
      {
        name: "preview_site",
        purpose: "Run the site and return its preview to the editor.",
      },
      {
        name: "publish_site",
        purpose: "Publish the reviewed version when the customer approves.",
      },
    ],
    outcome:
      "A working preview, editable source, and a deliberate publish step.",
    integration:
      "Give each customer an isolated workspace. Offer the sign-in methods supported by its agent, including Codex’s ChatGPT connection. Stream the work into your editor and expose previews through your own product. Keep the user’s account and project scoped to their workspace.",
  },
  {
    slug: "customer-support",
    title: "A support platform",
    headline: "From a support ticket to a resolved problem.",
    category: "Customer support",
    brand: "Relay",
    platform: "Cloud",
    kind: "support",
    summary:
      "An agent inside your help desk investigates a case, checks the account, and prepares the right action for a teammate to approve.",
    opportunity:
      "A good reply often needs more than the conversation. Your product can give an agent the customer record, delivery events, and the tools to fix what actually went wrong.",
    request:
      "Find out why the customer’s export never arrived. Fix the delivery issue and prepare a reply I can send.",
    reply:
      "The export finished, but delivery used the old email address. The verified address is correct now. I prepared a new delivery and a reply for your review.",
    context: [
      "Ticket #1048",
      "Customer account & delivery log",
      "Your support policy",
    ],
    tools: [
      {
        name: "get_customer",
        purpose: "Check the customer and their verified contact information.",
      },
      {
        name: "inspect_delivery",
        purpose: "Read export status and delivery events.",
      },
      {
        name: "redeliver_export",
        purpose: "Deliver the existing export after approval.",
      },
      {
        name: "reply_to_ticket",
        purpose: "Send the reviewed reply and update the case.",
      },
    ],
    outcome: "The investigation, the fix, and the customer reply in one place.",
    integration:
      "Connect the teammate’s supported agent account in a private workspace. Pass the selected ticket as context and expose narrowly scoped account and delivery tools. Render the proposed action in the ticket, then execute it only after the teammate approves.",
  },
  {
    slug: "analytics-workspace",
    title: "An analytics product",
    headline: "Ask a business question. Get a working analysis.",
    category: "Data & analytics",
    brand: "Metric",
    platform: "Desktop",
    kind: "analytics",
    summary:
      "Use an existing local agent to investigate business data, write the analysis, and create an interactive dashboard in your application.",
    opportunity:
      "Your customers have data and questions. Let them describe the analysis they need while an agent inspects the schema, writes the query, and turns the result into a view they can keep using.",
    request:
      "Compare our sales channels this quarter. Build a dashboard I can filter by month, and explain where the growth came from.",
    reply:
      "Direct sales grew from $18k to $27k, contributing $9k of the quarter’s $13k increase in monthly revenue. I saved a dashboard with a month filter and the source rows.",
    context: [
      "quarterly-sales.csv",
      "Channel definitions",
      "Your chart components",
    ],
    tools: [
      {
        name: "inspect_dataset",
        purpose: "Read the schema and validate the source data.",
      },
      {
        name: "query_dataset",
        purpose: "Run a read-only query against the selected dataset.",
      },
      {
        name: "save_dashboard",
        purpose: "Save the chart, filters, and source query in your app.",
      },
    ],
    outcome: "An interactive dashboard with traceable numbers.",
    integration:
      "Start a local runtime with the customer’s existing connection. Restrict data access to their selected dataset and expose your chart schema. The agent produces the analysis and dashboard definition; your renderer makes it a first-class view in the product.",
  },
  {
    slug: "learning-studio",
    title: "A learning platform",
    headline: "A course platform that helps create the course.",
    category: "Education & training",
    brand: "Tempo",
    platform: "Cloud",
    kind: "learning",
    summary:
      "Let instructors turn their material into interactive lessons, exercises, and quizzes—saved directly in your course library.",
    opportunity:
      "Your platform already knows how to deliver a course. Give instructors an agent that can build one: work through their teaching notes, create a playable exercise, and check that the lesson works.",
    request:
      "Turn my piano notes into a beginner course. Add a keyboard students can play, a chord exercise, and a short quiz.",
    reply:
      "First chords is ready. It includes a playable keyboard, a chord-building exercise, and a scored quiz. Try the lesson before adding it to your course.",
    context: [
      "piano-notes.md",
      "Your teaching guidelines",
      "Course library & lesson format",
    ],
    tools: [
      {
        name: "read_material",
        purpose: "Use the instructor’s notes and learning objectives.",
      },
      {
        name: "preview_lesson",
        purpose: "Run and check the interactive lesson.",
      },
      {
        name: "save_course",
        purpose: "Save the approved lesson to the instructor’s library.",
      },
    ],
    outcome: "A course students can actually play, practice, and learn from.",
    integration:
      "Connect the instructor’s agent account to their workspace. Supply the teaching material and your lesson conventions, then run generated exercises in an isolated preview. Save reviewed lessons through your course API and keep editing in your own interface.",
  },
  {
    slug: "commerce-workspace",
    title: "A commerce workspace",
    headline: "Your catalog. Ready for the next collection.",
    category: "Commerce operations",
    brand: "Shelf",
    platform: "Cloud",
    kind: "commerce",
    summary:
      "An agent works through a merchant’s catalog, creates the missing content, and stages product updates for review inside your commerce app.",
    opportunity:
      "Merchants should be able to ask for a better catalog without moving spreadsheets between tools. Let an agent inspect the collection, apply the brand’s writing rules, and prepare changes against real product records.",
    request:
      "Get the linen collection ready. Clean up the product titles and write the missing descriptions in our brand voice. Let me review before changing anything.",
    reply:
      "I prepared three title updates and two descriptions. Prices, inventory, and product URLs stay as they are. Review the changes below and choose what to apply.",
    context: [
      "Linen collection / 3 products",
      "Brand writing guidelines",
      "Existing product records",
    ],
    tools: [
      {
        name: "read_collection",
        purpose: "Read selected products and their current content.",
      },
      {
        name: "stage_product_updates",
        purpose: "Prepare a field-level change set for review.",
      },
      {
        name: "apply_product_updates",
        purpose: "Write only the changes the merchant selected.",
      },
    ],
    outcome:
      "Reviewed changes to real product records, with less repetitive editing.",
    integration:
      "Connect an agent in the merchant’s workspace and scope its tools to the chosen collection. Stream proposed edits into your product review UI. Your application validates and applies the approved changes. Add scheduled catalog checks later using the same integration.",
  },
];

export function getUseCase(slug: string) {
  return useCases.find((item) => item.slug === slug);
}

/** Retire the old prompt directory without stranding its existing links. */
export const legacyUseCases: Record<string, string> = {
  "personal-secretary": "",
  "weekly-planner": "",
  "meal-planner": "",
  "travel-companion": "",
  "home-organizer": "",
  "spending-tracker": "analytics-workspace",
  "meeting-companion": "desktop-research",
  "reply-drafter": "customer-support",
  "project-coordinator": "",
  "freelance-assistant": "",
  "spreadsheet-helper": "analytics-workspace",
  "research-companion": "desktop-research",
  "study-partner": "learning-studio",
  "reading-companion": "desktop-research",
  "language-practice": "learning-studio",
  "content-planner": "commerce-workspace",
  "newsletter-assistant": "",
  "presentation-builder": "",
  "idea-partner": "",
  "personal-app-builder": "cloud-site-builder",
  "website-builder": "cloud-site-builder",
  "repetitive-task-helper": "commerce-workspace",
  "developer-companion": "cloud-site-builder",
  "knowledge-library": "desktop-research",
};
