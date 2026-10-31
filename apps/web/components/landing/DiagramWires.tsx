"use client";

import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import styles from "./architecture-diagram.module.css";

type Point = { x: number; y: number };
type Wire = { path: string; start: Point; end: Point };

/** Connect measured panels through resizing and responsive stacking. */
export default function DiagramWires({ children, className = "" }: { children: ReactNode; className?: string }) {
  const root = useRef<HTMLDivElement>(null);
  const [wires, setWires] = useState<Wire[]>([]);
  useLayoutEffect(() => {
    const element = root.current;
    if (!element) return;
    const panels = [...element.querySelectorAll<HTMLElement>("[data-signal]")];
    const measure = () => {
      const board = element.getBoundingClientRect();
      const desktop = window.innerWidth >= 1024;
      const point = (name: string, side: "left" | "right" | "top" | "bottom", fraction = .5): Point => {
        const rect = panels.find((panel) => panel.dataset.signal === name)!.getBoundingClientRect();
        return {
          x: rect.left - board.left + (side === "left" ? 0 : side === "right" ? rect.width : rect.width * fraction),
          y: rect.top - board.top + (side === "top" ? 0 : side === "bottom" ? rect.height : rect.height * fraction),
        };
      };
      const wire = (start: Point, end: Point): Wire => {
        const mid = desktop ? (start.x + end.x) / 2 : (start.y + end.y) / 2;
        return { start, end, path: desktop
          ? `M${start.x} ${start.y}C${mid} ${start.y} ${mid} ${end.y} ${end.x} ${end.y}`
          : `M${start.x} ${start.y}C${start.x} ${mid} ${end.x} ${mid} ${end.x} ${end.y}` };
      };
      setWires(desktop ? [
        ...["files", "website", "workspace"].map((name, i) => wire(point(name, "right"), point("hub", "left", .3 + i * .2))),
        ...["agents", "api"].map((name, i) => wire(point("hub", "right", .35 + i * .3), point(name, "left"))),
      ] : [
        wire(point("sources", "bottom"), point("hub", "top")),
        wire(point("hub", "bottom"), point("agents", "top")),
        ...(window.innerWidth < 768 ? [wire(point("agents", "bottom"), point("api", "top"))] : [wire(point("hub", "bottom"), point("api", "top"))]),
      ]);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    panels.forEach((panel) => observer.observe(panel));
    window.addEventListener("resize", measure);
    measure();
    return () => { observer.disconnect(); window.removeEventListener("resize", measure); };
  }, []);
  return (
    <div ref={root} className={`${styles.board} ${className}`}>
      <svg className={styles.wires} width="100%" height="100%" aria-hidden="true">
        {wires.map(({ path, start, end }, index) => (
          <g key={index}>
            <path d={path} className={styles.wire} />
            <path d={path} className={styles.signal} pathLength={100} style={{ animationDelay: `${index * -.8}s` }} />
            <circle cx={start.x} cy={start.y} r={3} className={styles.socket} />
            <circle cx={end.x} cy={end.y} r={3} className={styles.socket} />
          </g>
        ))}
      </svg>
      {children}
    </div>
  );
}
