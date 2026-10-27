"use client";

import { useRef, useState } from "react";
import { ArrowRight, Search, X } from "lucide-react";
import {
  useCaseCategories,
  useCases,
  type UseCaseCategory,
} from "@/lib/use-cases";
import UseCaseCard from "./UseCaseCard";
import styles from "./use-cases.module.css";

export default function UseCaseExplorer() {
  const [category, setCategory] = useState<UseCaseCategory | "All">("All");
  const [query, setQuery] = useState("");
  const search = useRef<HTMLInputElement>(null);
  const terms = query.toLocaleLowerCase().trim().split(/\s+/).filter(Boolean);
  const orderedCases = [...useCases].sort((a, b) => {
    if (a.slug === "repetitive-task-helper") return 1;
    if (b.slug === "repetitive-task-helper") return -1;
    return (
      useCaseCategories.indexOf(a.category) -
      useCaseCategories.indexOf(b.category)
    );
  });
  const filtered = orderedCases.filter((item) => {
    const content = [
      item.title,
      item.summary,
      item.category,
      ...item.inputs,
      ...item.outcomes,
    ]
      .join(" ")
      .toLocaleLowerCase();
    return (
      (category === "All" || item.category === category) &&
      terms.every((term) => content.includes(term))
    );
  });

  return (
    <section
      id="use-case-library"
      className={styles.library}
      aria-labelledby="library-title"
    >
      <div className={styles.libraryHeading}>
        <div>
          <span className={styles.eyebrow}>Made possible in your product.</span>
          <h2 id="library-title">What will your users create?</h2>
        </div>
        <p>
          Find an idea, borrow a starting prompt,
          <br />
          and make it your own.
        </p>
      </div>
      <div className={styles.filters}>
        <div
          role="group"
          aria-label="Filter use cases by category"
          className={styles.categories}
        >
          {(["All", ...useCaseCategories] as const).map((value) => (
            <button
              type="button"
              key={value}
              aria-pressed={category === value}
              onClick={() => setCategory(value)}
            >
              {value}
            </button>
          ))}
        </div>
        <div className={styles.search}>
          <Search size={16} aria-hidden="true" />
          <label className="sr-only" htmlFor="use-case-search">
            Search use cases
          </label>
          <input
            ref={search}
            id="use-case-search"
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Find an idea…"
          />
          {query && (
            <button
              type="button"
              aria-label="Clear search"
              onClick={() => {
                setQuery("");
                search.current?.focus();
              }}
            >
              <X size={15} />
            </button>
          )}
        </div>
      </div>
      <p
        className={styles.resultCount}
        role="status"
        aria-live="polite"
        aria-atomic="true"
      >
        {filtered.length} of {useCases.length} ideas
        {category !== "All" ? ` · ${category}` : ""}
        {query.trim() ? ` matching “${query.trim()}”` : ""}
      </p>
      {filtered.length ? (
        <div className={styles.caseGrid}>
          {filtered.map((item) => (
            <UseCaseCard key={item.slug} item={item} />
          ))}
        </div>
      ) : (
        <div className={styles.empty}>
          <Search size={40} strokeWidth={1.25} />
          <h3>Let’s try another idea.</h3>
          <p>
            No use cases match these filters. Try a shorter search or explore
            the full collection.
          </p>
          <button
            type="button"
            onClick={() => {
              setCategory("All");
              setQuery("");
              search.current?.focus();
            }}
          >
            Show all ideas
            <ArrowRight size={15} />
          </button>
        </div>
      )}
    </section>
  );
}
