import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import {
  ArrowLeft,
  ArrowRight,
  ArrowUpRight,
  Check,
  MessageSquare,
} from "lucide-react";
import { getUseCase, useCases, useCaseTone } from "@/lib/use-cases";
import UseCaseArt from "@/components/landing/UseCaseArt";
import UseCaseCard from "@/components/landing/UseCaseCard";
import CopyButton from "@/components/landing/CopyButton";
import base from "@/components/landing/landing.module.css";
import styles from "@/components/landing/use-cases.module.css";

type Props = { params: Promise<{ slug: string }> };

export function generateStaticParams() {
  return useCases.map(({ slug }) => ({ slug }));
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const item = getUseCase((await params).slug);
  if (!item) notFound();
  return {
    title: `${item.title} — Mindwire examples`,
    description: item.summary,
    alternates: { canonical: `/use-cases/${item.slug}/` },
  };
}

export default async function UseCasePage({ params }: Props) {
  const item = getUseCase((await params).slug);
  if (!item) notFound();
  const related = useCases
    .filter(
      (other) => other.category === item.category && other.slug !== item.slug,
    )
    .slice(0, 3);

  return (
    <div className={base.landing}>
      <div className={styles.detail} data-tone={useCaseTone(item.category)}>
        <Link className={styles.backLink} href="/use-cases#use-case-library">
          <ArrowLeft size={15} />
          All examples
        </Link>
        <section className={styles.detailHero} aria-labelledby="case-title">
          <div>
            <span className={styles.eyebrow}>
              {item.category} / {item.title}
            </span>
            <h1 id="case-title">{item.headline}</h1>
            <p>{item.summary}</p>
          </div>
          <div className={styles.detailArt}>
            <UseCaseArt kind={item.art} />
            <span>{item.title}</span>
          </div>
        </section>

        <section
          className={styles.recipe}
          aria-label={`Get started with a ${item.title.toLowerCase()}`}
        >
          <div className={styles.promptCard}>
            <div className={styles.promptHeading}>
              <MessageSquare size={28} strokeWidth={1.5} />
              <div>
                <h2>Start the conversation.</h2>
                <p>Copy this prompt. Make it yours.</p>
              </div>
              <CopyButton value={item.prompt} label="Copy starter prompt" />
            </div>
            <blockquote>{item.prompt}</blockquote>
            <div className={styles.promptFooter}>
              <span>Try it in the Mindwire app.</span>
              <Link href="/get-started">
                Set up the app
                <ArrowUpRight size={15} />
              </Link>
            </div>
          </div>
          <div className={styles.contextList}>
            <h2>A little context helps.</h2>
            <p>Provide the files and details needed for the task.</p>
            <ul>
              {item.inputs.map((input, i) => (
                <li key={input}>
                  <span>0{i + 1}</span>
                  {input}
                </li>
              ))}
            </ul>
            <Link href="/docs/guides/quickstart" className={styles.textLink}>
              Build this into your product
              <ArrowUpRight size={14} />
            </Link>
          </div>
        </section>

        <section className={styles.outcomes} aria-labelledby="outcomes-title">
          <div>
            <span className={styles.eyebrow}>
              Something useful to come back to.
            </span>
            <h2 id="outcomes-title">
              From an idea
              <br />
              to something you can use.
            </h2>
            <p>
              Ask for these results, review them together, and refine the
              details until they fit.
            </p>
          </div>
          <ul>
            {item.outcomes.map((outcome) => (
              <li key={outcome}>
                <span>
                  <Check size={17} />
                </span>
                {outcome}
              </li>
            ))}
          </ul>
        </section>

        <section className={styles.followups} aria-labelledby="followups-title">
          <div>
            <h2 id="followups-title">Take it a little further.</h2>
            <p>The first result is a starting point.</p>
          </div>
          <div>
            {item.followups.map((prompt) => (
              <div className={styles.followupPrompt} key={prompt}>
                <blockquote>“{prompt}”</blockquote>
                <CopyButton
                  value={prompt}
                  label={`Copy follow-up: ${prompt}`}
                />
              </div>
            ))}
          </div>
        </section>

        <section className={styles.related} aria-labelledby="related-title">
          <div className={styles.relatedHeading}>
            <h2 id="related-title">A few more possibilities.</h2>
            <Link
              href="/use-cases#use-case-library"
              className={styles.textLink}
            >
              All {useCases.length} ideas
              <ArrowRight size={15} />
            </Link>
          </div>
          <div className={styles.caseGrid}>
            {related.map((other) => (
              <UseCaseCard key={other.slug} item={other} />
            ))}
          </div>
        </section>
        <div className={styles.detailClosing}>
          <div>
            <h2>Bring it into your product.</h2>
            <p>Your tools, your context, your interface.</p>
          </div>
          <Link href="/docs/guides/quickstart" className={base.primaryButton}>
            Start building
            <ArrowUpRight size={16} />
          </Link>
        </div>
      </div>
    </div>
  );
}
