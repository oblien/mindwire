export const useCaseCategories = [
  "Build",
  "Create",
  "Learning",
  "Work",
  "Everyday",
] as const;
export type UseCaseCategory = (typeof useCaseCategories)[number];
export type UseCaseArtKind =
  "calendar" | "notes" | "plan" | "map" | "chart" | "folders" | "book" | "app";
export type UseCase = {
  slug: string;
  title: string;
  headline: string;
  category: UseCaseCategory;
  art: UseCaseArtKind;
  summary: string;
  prompt: string;
  inputs: string[];
  outcomes: string[];
  followups: string[];
};

/** One catalog supplies the directory, detail pages, prompts and sitemap. */
export const useCases: UseCase[] = [
  {
    slug: "personal-secretary",
    title: "Personal secretary",
    headline: "A little less on your plate.",
    category: "Everyday",
    art: "calendar",
    summary:
      "Bring your priorities, meeting notes and loose ends into one clear daily brief.",
    prompt:
      "Be my personal secretary. Use the notes, commitments and availability I share to prepare a short daily brief: my three priorities, preparation for upcoming meetings, and follow-ups worth drafting. Leave room for focused work. Ask about missing times instead of inventing appointments. Keep message drafts ready for my review and save the brief in my workspace.",
    inputs: [
      "Your commitments and available time",
      "Meeting notes and open follow-ups",
      "Your priorities and preferred writing style",
    ],
    outcomes: [
      "A daily brief with three clear priorities",
      "Meeting preparation and useful questions",
      "Follow-up drafts you can review",
    ],
    followups: [
      "Turn these meeting notes into tomorrow’s follow-ups.",
      "My afternoon changed. Help me reshuffle the plan.",
    ],
  },
  {
    slug: "weekly-planner",
    title: "Weekly planner",
    headline: "Make room for the week you want.",
    category: "Everyday",
    art: "plan",
    summary:
      "Turn a long list of intentions into a realistic week, with a little breathing room.",
    prompt:
      "Help me plan my week from the tasks and fixed commitments I provide. Ask which outcomes matter most, estimate manageable work blocks, and leave buffer time. Separate must-dos from nice-to-haves. Make a simple weekly plan I can revise, and tell me when I am trying to fit too much into one day.",
    inputs: [
      "Your task list and deadlines",
      "Fixed commitments and working hours",
      "Time you want to keep free",
    ],
    outcomes: [
      "A practical plan for the week",
      "A shorter list of real priorities",
      "A reusable weekly planning document",
    ],
    followups: [
      "Move the flexible tasks out of Friday.",
      "Turn the plan into a small checklist I can reuse.",
    ],
  },
  {
    slug: "meal-planner",
    title: "Meal planner",
    headline: "Dinner, without the daily debate.",
    category: "Everyday",
    art: "plan",
    summary:
      "Plan simple meals around your preferences, available time and the ingredients you already have.",
    prompt:
      "Build me a simple meal planner. Start by asking what is in my kitchen, how many people I cook for, my dietary preferences and how much time I have. Suggest a week of dinners, combine the ingredients into a shopping list, and let me swap a meal without rebuilding the whole plan.",
    inputs: [
      "Ingredients you already have",
      "Food preferences and portions",
      "Your cooking time and grocery budget",
    ],
    outcomes: [
      "A flexible menu for the week",
      "One combined shopping list",
      "A little tool for swapping meals",
    ],
    followups: [
      "Make Tuesday a fifteen-minute dinner.",
      "Use the leftovers from Monday in another meal.",
    ],
  },
  {
    slug: "travel-companion",
    title: "Travel companion",
    headline: "A little plan. A better adventure.",
    category: "Everyday",
    art: "map",
    summary:
      "Keep a trip’s plans, booking details and places worth exploring together.",
    prompt:
      "Help me organize a trip using the dates, booking confirmations and ideas I share. Build an itinerary with realistic travel time and space to wander. Keep addresses, reservation details and a packing checklist in one place. Mark opening times and prices that still need checking, and suggest alternatives for a rainy day.",
    inputs: [
      "Travel dates and booking details",
      "Places you want to visit",
      "Your pace, interests and budget",
    ],
    outcomes: [
      "An itinerary with room to explore",
      "A tidy collection of trip details",
      "A packing list and backup ideas",
    ],
    followups: [
      "Give us a slower version of Saturday.",
      "Make a small trip dashboard from this plan.",
    ],
  },
  {
    slug: "home-organizer",
    title: "Home organizer",
    headline: "A home for the things to remember.",
    category: "Everyday",
    art: "folders",
    summary:
      "Organize household documents, recurring chores and the details you always end up looking for.",
    prompt:
      "Help me organize the household notes and files I provide. Propose clear folders and a simple index for manuals, maintenance notes and important dates. Create a checklist of upcoming tasks with links to the relevant files. Show me the proposed organization before moving anything, and keep the original documents intact.",
    inputs: [
      "The household files you choose",
      "Maintenance notes and recurring tasks",
      "Your preferred folder structure",
    ],
    outcomes: [
      "A searchable household index",
      "A maintenance and chores checklist",
      "An organization plan for your files",
    ],
    followups: [
      "Find the documents for the washing machine.",
      "Make a shared household checklist from these notes.",
    ],
  },
  {
    slug: "spending-tracker",
    title: "Spending tracker",
    headline: "See where the little things add up.",
    category: "Everyday",
    art: "chart",
    summary:
      "Turn the expense files you provide into clear categories and a personal spending view.",
    prompt:
      "Make a spending tracker from the expense spreadsheet I provide. Explain the columns you find, group the transactions into categories, and flag anything ambiguous for me to label. Create a weekly and monthly view with totals that reconcile to the original file. Let me change categories and add an expense manually.",
    inputs: [
      "An expense spreadsheet or CSV",
      "The categories you want to track",
      "A weekly or monthly spending plan",
    ],
    outcomes: [
      "Organized, traceable expense categories",
      "A visual summary of your spending",
      "A personal tracker you can update",
    ],
    followups: [
      "Show recurring expenses separately.",
      "Let me compare this month with the previous one.",
    ],
  },
  {
    slug: "meeting-companion",
    title: "Meeting companion",
    headline: "Less note chasing. More follow-through.",
    category: "Work",
    art: "notes",
    summary:
      "Prepare an agenda, make sense of meeting notes and keep decisions from disappearing.",
    prompt:
      "Work with the meeting notes or transcript I share. Pull out the decisions, open questions and action items. Keep each owner and date tied to what was actually said; flag missing information. Draft a concise recap and prepare an agenda for the next conversation. Save both as clear, editable documents.",
    inputs: [
      "Notes or a transcript you provide",
      "The meeting’s goal and participants",
      "Any existing action list",
    ],
    outcomes: [
      "A concise meeting recap",
      "Action items with owners and dates",
      "A focused agenda for next time",
    ],
    followups: [
      "Draft a recap in my usual tone.",
      "Which decisions still need an answer?",
    ],
  },
  {
    slug: "reply-drafter",
    title: "Reply drafter",
    headline: "The right words, a little sooner.",
    category: "Work",
    art: "notes",
    summary:
      "Turn the messages you share into thoughtful replies that sound like you.",
    prompt:
      "Help me draft replies to the messages I provide. Summarize what each sender needs, identify any deadlines, and write a clear response in my tone. Use the background I give you and ask when an answer depends on a missing fact. Keep the replies as drafts for me to review.",
    inputs: [
      "Messages or email threads you share",
      "The facts needed for a response",
      "Examples of your writing style",
    ],
    outcomes: [
      "A clear list of response priorities",
      "Reply drafts in your voice",
      "Questions to resolve before replying",
    ],
    followups: [
      "Make this warmer and half as long.",
      "Prepare a follow-up to the message we drafted.",
    ],
  },
  {
    slug: "project-coordinator",
    title: "Project coordinator",
    headline: "Keep the moving pieces together.",
    category: "Work",
    art: "plan",
    summary:
      "Bring milestones, decisions and open tasks into a project plan your team can actually use.",
    prompt:
      "Turn these project notes into a useful working plan. Identify milestones, dependencies, owners and unanswered questions. Distinguish agreed commitments from suggestions. Create a small status board and a short weekly update based on the information I provide, with clear links back to the source notes.",
    inputs: [
      "Your project brief and milestones",
      "Team notes and known dependencies",
      "Current tasks and status updates",
    ],
    outcomes: [
      "A practical project status board",
      "A visible list of blockers",
      "A concise weekly update draft",
    ],
    followups: [
      "Show what is blocking the next milestone.",
      "Prepare an update for the client from these notes.",
    ],
  },
  {
    slug: "freelance-assistant",
    title: "Freelance assistant",
    headline: "More time for the work you love.",
    category: "Work",
    art: "folders",
    summary:
      "Keep client briefs, deliverables and follow-ups organized around each engagement.",
    prompt:
      "Help me organize this client engagement. Use the brief and correspondence I share to outline the scope, deliverables, open questions and review dates. Draft a proposal using only the rates and terms I provide. Keep a checklist of what is agreed and what still needs the client’s answer.",
    inputs: [
      "A client brief and relevant messages",
      "Your rates and proposed scope",
      "Delivery dates and review stages",
    ],
    outcomes: [
      "A clear scope and deliverable list",
      "A proposal draft ready to refine",
      "A client follow-up checklist",
    ],
    followups: [
      "Flag where this new request changes the scope.",
      "Draft a friendly reminder about the review date.",
    ],
  },
  {
    slug: "spreadsheet-helper",
    title: "Spreadsheet helper",
    headline: "From messy rows to something useful.",
    category: "Work",
    art: "chart",
    summary:
      "Clean up spreadsheets, check totals and turn a pile of rows into a clear explanation.",
    prompt:
      "Inspect this spreadsheet and explain its structure. Find duplicate rows, inconsistent labels and missing values. Propose the cleanup rules first, then create a cleaned copy with a change log. Check the totals against the original and make a few useful charts that answer the questions I give you.",
    inputs: [
      "Your spreadsheet or CSV files",
      "The questions you want to answer",
      "Rules for categories and duplicates",
    ],
    outcomes: [
      "A cleaned copy of your data",
      "A record of the changes made",
      "Charts and an understandable summary",
    ],
    followups: [
      "Show which rows need a human decision.",
      "Turn this cleanup into a repeatable tool.",
    ],
  },
  {
    slug: "research-companion",
    title: "Research companion",
    headline: "Get to the useful part of the reading.",
    category: "Learning",
    art: "book",
    summary:
      "Compare the material you collect, trace the evidence and keep useful findings together.",
    prompt:
      "Help me research this question using the sources I share and any research tools available in this workspace. Compare the main claims, preserve source references, and separate evidence from interpretation. Tell me where the sources disagree or leave gaps. Create a short brief and a reading list for the next questions.",
    inputs: [
      "A specific research question",
      "Articles, documents or source links",
      "The depth and format you need",
    ],
    outcomes: [
      "A brief with traceable sources",
      "A comparison of key findings",
      "Open questions and a reading list",
    ],
    followups: [
      "Which conclusion has the weakest evidence?",
      "Compare these two new sources with the brief.",
    ],
  },
  {
    slug: "study-partner",
    title: "Study partner",
    headline: "Make the hard part click.",
    category: "Learning",
    art: "book",
    summary:
      "Turn your course notes into explanations, practice questions and a study plan that fits you.",
    prompt:
      "Be my study partner for the material I share. Ask what I already understand, explain difficult ideas with concrete examples, and give me practice questions one at a time. Let me try before revealing the answer. Track the concepts I need to revisit and make a short plan for my next study session.",
    inputs: [
      "Your notes or course material",
      "What you are struggling with",
      "Your available study time",
    ],
    outcomes: [
      "Explanations at your level",
      "Practice questions with feedback",
      "A focused revision plan",
    ],
    followups: [
      "Give me a new problem on the part I missed.",
      "Build a little quiz from these chapters.",
    ],
  },
  {
    slug: "reading-companion",
    title: "Reading companion",
    headline: "Keep more of what stays with you.",
    category: "Learning",
    art: "book",
    summary:
      "Organize reading notes, connect interesting ideas and make a home for your next chapter.",
    prompt:
      "Help me organize the reading notes and book list I provide. Keep summaries tied to my notes, connect recurring ideas across books, and collect questions I want to think about. Make a simple reading tracker with shelves, a weekly goal and room for my own observations.",
    inputs: [
      "Your book list and reading notes",
      "Topics you want to explore",
      "Your reading goal",
    ],
    outcomes: [
      "A connected collection of notes",
      "A reading list organized by interest",
      "A personal reading tracker",
    ],
    followups: [
      "Find ideas that appear in more than one book.",
      "Add a place for my favorite passages.",
    ],
  },
  {
    slug: "language-practice",
    title: "Language practice",
    headline: "A little practice, every conversation.",
    category: "Learning",
    art: "notes",
    summary:
      "Practice real situations, get explanations and keep a personal vocabulary list.",
    prompt:
      "Help me practice the language and level I choose. Start a short conversation about an everyday situation. Give me time to answer, then explain a useful correction without interrupting every sentence. Keep new vocabulary with examples, and finish with a small exercise based on the mistakes I made.",
    inputs: [
      "Your language and current level",
      "Situations you want to practice",
      "How you prefer corrections",
    ],
    outcomes: [
      "Practice shaped around real situations",
      "Clear explanations of corrections",
      "A personal vocabulary notebook",
    ],
    followups: [
      "Let’s practice ordering at a restaurant.",
      "Make a review game from this week’s vocabulary.",
    ],
  },
  {
    slug: "content-planner",
    title: "Content planner",
    headline: "Give your good ideas a little structure.",
    category: "Create",
    art: "calendar",
    summary:
      "Shape rough ideas into a manageable editorial plan, with drafts that keep your voice.",
    prompt:
      "Help me plan content from the ideas and source material I provide. Ask about the audience, channels and tone. Propose a realistic editorial calendar, develop a few strong angles, and draft the first piece for my review. Keep factual claims tied to the source material and leave publishing to me.",
    inputs: [
      "Your ideas and source material",
      "Your audience and preferred channels",
      "Voice examples and publishing cadence",
    ],
    outcomes: [
      "A practical editorial calendar",
      "Clear angles for each piece",
      "Drafts ready for your review",
    ],
    followups: [
      "Turn this long piece into three short posts.",
      "Give this draft a stronger opening.",
    ],
  },
  {
    slug: "newsletter-assistant",
    title: "Newsletter assistant",
    headline: "Turn a week of ideas into a good read.",
    category: "Create",
    art: "notes",
    summary:
      "Collect links, find a thread and shape a newsletter worth opening.",
    prompt:
      "Help me assemble a newsletter from the notes and links I share. Find a coherent theme, propose an outline and write a draft in my voice. Preserve links to the original sources and flag details that need checking. Suggest subject lines and create a clean, editable version for me to publish.",
    inputs: [
      "Saved links and rough notes",
      "A past issue or writing sample",
      "The audience and issue length",
    ],
    outcomes: [
      "An issue with a clear thread",
      "Subject line options",
      "An editable newsletter draft",
    ],
    followups: [
      "Make this issue more useful and less promotional.",
      "Create a simple archive page for these issues.",
    ],
  },
  {
    slug: "presentation-builder",
    title: "Presentation builder",
    headline: "Make the story easier to see.",
    category: "Create",
    art: "chart",
    summary:
      "Turn a brief, notes and data into a presentation with a clear point.",
    prompt:
      "Help me turn this material into a presentation. Ask who it is for and what they should understand or decide. Build a clear narrative, outline each slide, suggest visuals from the data I provide, and draft speaker notes. Create an editable browser presentation or slide outline using the tools available here.",
    inputs: [
      "Your brief, notes and data",
      "The audience and desired outcome",
      "Time limit and visual preferences",
    ],
    outcomes: [
      "A clear presentation narrative",
      "Slide content and speaker notes",
      "An editable presentation or outline",
    ],
    followups: [
      "Cut this down to a five-minute version.",
      "Make the opening example more concrete.",
    ],
  },
  {
    slug: "idea-partner",
    title: "Idea partner",
    headline: "A place for the what-ifs.",
    category: "Create",
    art: "plan",
    summary:
      "Explore an idea, challenge its assumptions and find a small first version worth making.",
    prompt:
      "Think through this idea with me. Ask what problem I am trying to solve and who it is for. Explore a few directions, point out important assumptions and help me choose a small first version. Turn that version into a brief with a practical next step, rather than a long list of possibilities.",
    inputs: [
      "The idea, however rough",
      "Who it should help",
      "Your time and practical constraints",
    ],
    outcomes: [
      "A clearer problem to solve",
      "A focused first-version brief",
      "A useful next step to try",
    ],
    followups: [
      "What could I test in a single afternoon?",
      "Build a small prototype of the direction we chose.",
    ],
  },
  {
    slug: "personal-app-builder",
    title: "Personal app builder",
    headline: "That little app you wish existed.",
    category: "Build",
    art: "app",
    summary:
      "Make a useful little app around the way you actually work or live.",
    prompt:
      "Help me build a small app for the problem I describe. Ask about the essentials, propose the simplest useful version, and build it in this workspace. Make it comfortable on my phone and computer, try the main interactions, and explain how to run it. Keep a list of ideas we can add after the first version works.",
    inputs: [
      "The problem you want to solve",
      "Your essential screens or actions",
      "Any sample data and style references",
    ],
    outcomes: [
      "A working first version",
      "A usable interface for your task",
      "Run instructions and next ideas",
    ],
    followups: [
      "Make it easier to use with one hand.",
      "Add an export so I can keep a copy of my data.",
    ],
  },
  {
    slug: "website-builder",
    title: "Website builder",
    headline: "Give your thing a place on the web.",
    category: "Build",
    art: "app",
    summary:
      "Build a portfolio, project page or small website from your own content and direction.",
    prompt:
      "Build a website from the content and visual references I provide. Start with the page structure and main message. Create a responsive version, check keyboard navigation and the important links, and help me refine the details. Show me the local preview and explain what is needed to publish it.",
    inputs: [
      "Your page copy and images",
      "A few style references",
      "The pages and actions you need",
    ],
    outcomes: [
      "A working website preview",
      "Layouts for phone and desktop",
      "Clear instructions for publishing",
    ],
    followups: [
      "Make the homepage tell the story more clearly.",
      "Add a page for this new project.",
    ],
  },
  {
    slug: "repetitive-task-helper",
    title: "Repetitive task helper",
    headline: "Do the useful part. Skip the repetition.",
    category: "Build",
    art: "folders",
    summary:
      "Turn a repeatable file or data chore into a small tool you can run again.",
    prompt:
      "Help me automate the repeatable task I describe. Inspect a small sample, explain the steps and propose a simple tool. Include a preview of the changes before applying them, keep the originals available, and test on a copy first. Write down how to run it again and how to spot an input it cannot handle.",
    inputs: [
      "The steps you currently repeat",
      "A small set of sample files",
      "Your rules and expected result",
    ],
    outcomes: [
      "A reusable tool for the task",
      "A preview before changes are applied",
      "Instructions and a tested example",
    ],
    followups: [
      "Let me choose the folder from a small interface.",
      "Add a summary of what changed after each run.",
    ],
  },
  {
    slug: "developer-companion",
    title: "Developer companion",
    headline: "An extra pair of hands in your project.",
    category: "Build",
    art: "app",
    summary:
      "Work through a feature, investigate a bug or refactor with your project’s context in reach.",
    prompt:
      "Help me with this project. Read its instructions and the relevant files, explain the approach, and make the smallest coherent change for the task. Follow the existing patterns, run the relevant checks, and show me what changed and what still needs attention. Ask when a product decision would change the scope.",
    inputs: [
      "Your project folder or repository",
      "The feature request or bug report",
      "Project instructions and test commands",
    ],
    outcomes: [
      "Changes you can inspect in Git",
      "Relevant checks and their results",
      "A clear explanation of the work",
    ],
    followups: [
      "Review this change for missed edge cases.",
      "Walk me through the files before I commit.",
    ],
  },
  {
    slug: "knowledge-library",
    title: "Knowledge library",
    headline: "Make your notes useful again.",
    category: "Build",
    art: "book",
    summary:
      "Build a small, searchable home for documents, reference material and things you want to keep.",
    prompt:
      "Help me turn the notes and documents I choose into a useful knowledge library. Propose topics and tags, preserve the source files, and create an index that links back to them. Build a simple interface for searching and browsing. When summarizing a document, show where the information came from and flag uncertain details.",
    inputs: [
      "The documents you want to include",
      "Topics and tags that make sense to you",
      "How you want to browse the material",
    ],
    outcomes: [
      "A searchable collection of documents",
      "An index with source references",
      "A small interface built around your notes",
    ],
    followups: [
      "Find the connections between these two topics.",
      "Add these new notes without reorganizing everything.",
    ],
  },
];

export function getUseCase(slug: string) {
  return useCases.find((item) => item.slug === slug);
}

export function useCaseTone(category: UseCaseCategory) {
  return category === "Everyday" || category === "Create"
    ? "peach"
    : category === "Learning"
      ? "mint"
      : "blue";
}
