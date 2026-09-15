// Renders a normalized `Block[]` (from live events or reloaded history) as an assistant turn. One
// renderer, so streaming and history look identical. Interactions are answerable only while a live run
// is attached (`onRespond` present); in reloaded history they render read-only.
import { useState } from "react";
import {
  ChevronRight,
  FilePenLine,
  TerminalSquare,
  Search,
  Globe,
  Plug,
  Wrench,
  CircleCheck,
  CircleX,
  Layers,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import type {
  Block,
} from "@/components/chat/blocks";
import type { ToolEvent, Interaction, Question, QuestionAnswer, CompactionInfo, RespondInput } from "@shared/api";

export function TurnView({
  blocks,
  onRespond,
}: {
  blocks: Block[];
  onRespond?: (input: RespondInput) => void;
}) {
  return (
    <div className="space-y-3">
      {blocks.map((b, i) => {
        switch (b.kind) {
          case "text":
            return <Prose key={i} text={b.text} />;
          case "thinking":
            return <Thinking key={i} text={b.text} />;
          case "tool":
            return <ToolCard key={i} tool={b.tool} />;
          case "interaction":
            return <InteractionCard key={i} interaction={b.interaction} onRespond={onRespond} />;
          case "compaction":
            return <CompactionMarker key={i} info={b.compaction} />;
          case "result":
            return <ResultChip key={i} isError={b.result.isError} text={b.result.text} />;
        }
      })}
    </div>
  );
}

// ---- text -------------------------------------------------------------------
// Lightweight, safe rendering: split fenced code from prose. No HTML injection, no markdown lib.

function Prose({ text }: { text: string }) {
  const segments = text.split(/```/);
  return (
    <div className="space-y-2 text-sm leading-relaxed">
      {segments.map((seg, i) =>
        i % 2 === 1 ? (
          <pre
            key={i}
            className="overflow-x-auto border border-border bg-muted/50 p-3 font-mono text-xs"
          >
            <code>{seg.replace(/^[a-zA-Z0-9]*\n/, "")}</code>
          </pre>
        ) : (
          seg && (
            <p key={i} className="whitespace-pre-wrap break-words">
              {seg}
            </p>
          )
        ),
      )}
    </div>
  );
}

function Thinking({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="border-l-2 border-border pl-3">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
      >
        <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
        Thinking
      </button>
      {open && (
        <p className="mt-1 whitespace-pre-wrap break-words text-xs italic text-muted-foreground">
          {text}
        </p>
      )}
    </div>
  );
}

// ---- tools ------------------------------------------------------------------

const KIND_ICON = {
  file_edit: FilePenLine,
  file_read: FilePenLine,
  shell: TerminalSquare,
  search: Search,
  web_search: Globe,
  web_fetch: Globe,
  mcp: Plug,
} as const;

function ToolCard({ tool }: { tool: ToolEvent }) {
  const [open, setOpen] = useState(false);
  const action = tool.action;
  const kind = action?.kind ?? "other";
  const Icon = (KIND_ICON as Record<string, typeof Wrench>)[kind] ?? Wrench;
  const title = action?.title || tool.name || kind;

  return (
    <div className="border border-border">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs"
      >
        <Icon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-mono">{title}</span>
        {tool.isError ? (
          <CircleX className="size-3.5 shrink-0 text-destructive" />
        ) : tool.output !== undefined ? (
          <CircleCheck className="size-3.5 shrink-0 text-muted-foreground" />
        ) : null}
        <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
      </button>

      {open && (
        <div className="border-t border-border px-3 py-2 text-xs">
          <ToolActionBody tool={tool} />
        </div>
      )}
    </div>
  );
}

function ToolActionBody({ tool }: { tool: ToolEvent }) {
  const a = tool.action;

  if (a?.kind === "file_edit" && a.files?.length) {
    return (
      <div className="space-y-3">
        {a.files.map((f, i) => (
          <div key={i}>
            <div className="mb-1 flex items-center gap-2 font-mono">
              {f.op && <Badge variant="outline">{f.op}</Badge>}
              <span className="truncate">{f.path}</span>
            </div>
            {f.diff ? (
              <Diff diff={f.diff} />
            ) : (
              <p className="text-muted-foreground">No diff supplied.</p>
            )}
          </div>
        ))}
      </div>
    );
  }

  if (a?.kind === "shell" && a.shell) {
    return (
      <div className="space-y-2">
        {a.shell.command && (
          <pre className="overflow-x-auto bg-muted/50 p-2 font-mono">
            <code>$ {a.shell.command}</code>
          </pre>
        )}
        {(tool.output || a.shell.stdout) && (
          <pre className="max-h-64 overflow-auto whitespace-pre-wrap bg-muted/30 p-2 font-mono text-muted-foreground">
            <code>{tool.output ?? a.shell.stdout}</code>
          </pre>
        )}
        {a.shell.exitCode !== undefined && (
          <p className="text-muted-foreground">exit {a.shell.exitCode}</p>
        )}
      </div>
    );
  }

  if (a?.kind === "search" && a.search) {
    return (
      <p className="font-mono">
        {a.search.query ?? a.search.glob ?? ""}
        {a.search.path ? ` in ${a.search.path}` : ""}
      </p>
    );
  }

  if ((a?.kind === "web_search" || a?.kind === "web_fetch") && a.web) {
    return <p className="break-words font-mono">{a.web.url ?? a.web.query}</p>;
  }

  // Fallback: raw input/output.
  return <RawIO tool={tool} />;
}

function RawIO({ tool }: { tool: ToolEvent }) {
  return (
    <div className="space-y-2">
      {tool.input !== undefined && (
        <pre className="max-h-48 overflow-auto whitespace-pre-wrap bg-muted/50 p-2 font-mono">
          <code>
            {typeof tool.input === "string" ? tool.input : JSON.stringify(tool.input, null, 2)}
          </code>
        </pre>
      )}
      {tool.output !== undefined && (
        <pre className="max-h-64 overflow-auto whitespace-pre-wrap bg-muted/30 p-2 font-mono text-muted-foreground">
          <code>{tool.output}</code>
        </pre>
      )}
    </div>
  );
}

function Diff({ diff }: { diff: string }) {
  return (
    <pre className="max-h-72 overflow-auto bg-muted/30 p-2 font-mono leading-relaxed">
      {diff.split("\n").map((line, i) => {
        const tone = line.startsWith("+")
          ? "text-emerald-600 dark:text-emerald-400"
          : line.startsWith("-")
            ? "text-destructive"
            : line.startsWith("@@")
              ? "text-muted-foreground"
              : "";
        return (
          <div key={i} className={tone}>
            {line || " "}
          </div>
        );
      })}
    </pre>
  );
}

// ---- interactions -----------------------------------------------------------

function InteractionCard({
  interaction,
  onRespond,
}: {
  interaction: Interaction;
  onRespond?: (input: RespondInput) => void;
}) {
  const [feedback, setFeedback] = useState("");
  const [submitting, setSubmitting] = useState(false);
  if (interaction.kind === "todos") return <Todos interaction={interaction} />;
  const live = Boolean(onRespond && interaction.needsResponse && interaction.id);
  const answerable = live && !submitting;
  const questions: Question[] = interaction.questions?.length ? interaction.questions
    : ["choice", "select", "input"].includes(interaction.kind) ? [{
        id: interaction.id ?? "answer", title: interaction.title ?? "Your answer",
        options: interaction.options, multiSelect: interaction.kind === "select",
        allowOther: interaction.kind === "input" || interaction.meta?.allowOther === true,
        isSecret: interaction.meta?.isSecret === true,
      }] : [];
  const submit = async (reply: RespondInput) => {
    if (!answerable) return;
    setSubmitting(true);
    try { await onRespond?.({ ...reply, interactionId: interaction.id }); }
    finally { setSubmitting(false); }
  };

  return (
    <div className="border border-border p-3">
      <div className="mb-1 flex items-center gap-2">
        <Layers className="size-3.5 text-muted-foreground" />
        <span className="text-xs font-medium">{interaction.title ?? interaction.kind}</span>
      </div>
      {interaction.detail && (
        <p className="mb-2 whitespace-pre-wrap break-words text-xs text-muted-foreground">
          {interaction.detail}
        </p>
      )}

      {questions.length ? (
        <QuestionForm questions={questions} readOnly={!live} submitting={submitting} onSubmit={(answers) => {
          if (interaction.questions?.length) return submit({ answers });
          const answer = answers[questions[0].id];
          return submit({ options: answer?.options, text: answer?.text });
        }} />
      ) : interaction.options?.length ? (
        <div className="space-y-2">
          {interaction.needsResponse && interaction.feedback && (
            <Input value={feedback} onChange={(e) => setFeedback(e.target.value)} disabled={!answerable}
              placeholder={interaction.feedback === "rejection" ? "Reason if rejecting (optional)" : "Feedback (optional)"} />
          )}
          <div className="flex flex-wrap gap-2">
            {interaction.options.map((opt) => (
              <Button key={opt.id} size="sm" variant="outline" disabled={!answerable}
                className="h-auto whitespace-normal py-2 text-left"
                onClick={() => void submit({ decision: opt.id,
                  text: interaction.feedback === "always" || ["deny", "reject", "cancel"].includes(opt.id) ? feedback : undefined,
                })}>
                <span>{opt.label}{opt.description && <span className="block text-xs font-normal text-muted-foreground">{opt.description}</span>}</span>
              </Button>
            ))}
          </div>
        </div>
      ) : null}

      {!live && interaction.needsResponse && (
        <p className="mt-2 text-xs text-muted-foreground">This request is no longer accepting replies.</p>
      )}
    </div>
  );
}

function Todos({ interaction }: { interaction: Interaction }) {
  return (
    <div className="border border-border p-3">
      <p className="mb-2 text-xs font-medium">{interaction.title ?? "Plan"}</p>
      <ul className="space-y-1">
        {interaction.items?.map((t, i) => (
          <li key={i} className="flex items-center gap-2 text-xs">
            <span
              className={cn(
                "inline-block size-1.5 rounded-full",
                t.status === "completed"
                  ? "bg-foreground"
                  : t.status === "in_progress"
                    ? "bg-foreground/50"
                    : "bg-border",
              )}
            />
            <span className={cn(t.status === "completed" && "text-muted-foreground line-through")}>
              {t.content}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function QuestionForm({ questions, readOnly, submitting, onSubmit }: {
  questions: Question[];
  readOnly: boolean;
  submitting: boolean;
  onSubmit: (answers: Record<string, QuestionAnswer>) => Promise<void>;
}) {
  const [answers, setAnswers] = useState<Record<string, QuestionAnswer>>({});
  const disabled = readOnly || submitting;
  const complete = questions.every((q) => {
    const answer = answers[q.id];
    return (answer?.options?.length ?? 0) > 0 || q.optional
      || Boolean(answer?.text?.trim() && (q.allowOther || !q.options?.length));
  });
  const update = (id: string, value: Partial<QuestionAnswer>) =>
    setAnswers((old) => ({ ...old, [id]: { ...old[id], ...value } }));
  return (
    <form className="space-y-4" onSubmit={(event) => { event.preventDefault(); if (complete && !disabled) void onSubmit(answers); }}>
      {questions.map((q) => (
        <fieldset key={q.id} disabled={disabled} className="space-y-2">
          <legend className="text-sm font-medium">{q.title}</legend>
          {q.header && <p className="text-xs text-muted-foreground">{q.header}</p>}
          {q.options?.map((option) => (
            <div key={option.id}>
              <label className="flex cursor-pointer items-start gap-2 text-sm">
                <input className="mt-1" type={q.multiSelect ? "checkbox" : "radio"} name={q.id}
                  checked={answers[q.id]?.options?.includes(option.id) ?? false}
                  onChange={(event) => update(q.id, { options: q.multiSelect
                    ? event.target.checked ? [...(answers[q.id]?.options ?? []), option.id]
                      : (answers[q.id]?.options ?? []).filter((id) => id !== option.id)
                    : [option.id] })} />
                <span>{option.label}{option.description && <span className="block text-xs text-muted-foreground">{option.description}</span>}</span>
              </label>
              {option.preview && <details className="mt-1 text-xs text-muted-foreground"><summary>Preview</summary><pre className="whitespace-pre-wrap">{option.preview}</pre></details>}
            </div>
          ))}
          {!readOnly && <>
            {q.allowOther && Boolean(answers[q.id]?.options?.length) && <button type="button" className="text-xs underline" onClick={() => update(q.id, { options: [] })}>Clear selections</button>}
            <Input type={q.isSecret ? "password" : "text"} value={answers[q.id]?.text ?? ""}
              onChange={(event) => update(q.id, { text: event.target.value })}
              placeholder={!q.options?.length ? "Your answer" : q.allowOther ? "Feedback or another answer" : "Feedback (optional)"} />
          </>}
        </fieldset>
      ))}
      {!readOnly && <Button size="sm" type="submit" disabled={!complete || submitting}>{submitting ? "Sending…" : "Send answers"}</Button>}
    </form>
  );
}

// ---- misc -------------------------------------------------------------------

function CompactionMarker({ info }: { info: CompactionInfo }) {
  return (
    <div className="flex items-center gap-2 py-1 text-xs text-muted-foreground">
      <div className="h-px flex-1 bg-border" />
      <span>
        compacted{info.trigger ? ` (${info.trigger})` : ""}
        {info.preTokens && info.postTokens ? ` · ${info.preTokens}→${info.postTokens} tok` : ""}
      </span>
      <div className="h-px flex-1 bg-border" />
    </div>
  );
}

function ResultChip({ isError, text }: { isError?: boolean; text?: string }) {
  if (!isError && !text) return null;
  return (
    <div
      className={cn(
        "flex items-start gap-2 text-xs",
        isError ? "text-destructive" : "text-muted-foreground",
      )}
    >
      {isError ? (
        <CircleX className="mt-0.5 size-3.5 shrink-0" />
      ) : (
        <CircleCheck className="mt-0.5 size-3.5 shrink-0" />
      )}
      <span className="min-w-0 break-words">{text || (isError ? "Turn errored" : "Done")}</span>
    </div>
  );
}
