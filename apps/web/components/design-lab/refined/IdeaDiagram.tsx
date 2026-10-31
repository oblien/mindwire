"use client";

import { useLayoutEffect, useRef, useState } from "react";
import Link from "next/link";
import { ArrowRight, ArrowUpRight } from "lucide-react";
import AgentLogo from "@/components/marketing/AgentLogo";
import { productAgents, type ProductAgent } from "@/lib/product";
import { MindwireMark } from "./Artwork";
import { fluidJoin, type Anchor, type Edge } from "./fluid-join";
import styles from "./idea-diagram.module.css";

const inputs = [
  {
    id: "knowledge",
    title: "Files & knowledge.",
    detail: "The context behind every answer.",
    image: "knowledge-map",
    output: "Grounded in your knowledge.",
    explanation:
      "Give the agent the context that makes your product useful: your files, instructions, and knowledge.",
  },
  {
    id: "tools",
    title: "Tools & actions.",
    detail: "What your product can do.",
    image: "tool-routes",
    output: "Working within your boundaries.",
    explanation:
      "Connect your tools and decide how they work. Set boundaries and choose when the agent should ask for approval.",
  },
  {
    id: "interface",
    title: "Interface & brand.",
    detail: "Your signature, in every interaction.",
    image: "interface-frames",
    output: "Unmistakably your experience.",
    explanation:
      "Bring the agent into the interface your users already know, with your own voice, visual identity, and name.",
  },
] as const;

type Join = { id: string; d: string };

export default function IdeaDiagram() {
  const [active, setActive] = useState(0);
  const [agent, setAgent] = useState<ProductAgent>("codex");
  const [joins, setJoins] = useState<Join[]>([]);
  const board = useRef<HTMLDivElement>(null);
  const selectedInput = inputs[active];
  const chosenAgent = productAgents.find((item) => item.id === agent)!;

  useLayoutEffect(() => {
    const element = board.current;
    if (!element) return;
    let frame = 0;
    const measure = () => {
      const bounds = element.getBoundingClientRect();
      const compact = window.matchMedia("(max-width: 1099px)").matches;
      const anchor = (id: string, edge: Edge, fraction = 0.5): Anchor => {
        const node = element.querySelector<HTMLElement>(
          '[data-node="' + id + '"]',
        );
        if (!node) throw new Error("Missing connection surface: " + id);
        const rect = node.getBoundingClientRect();
        const directions = {
          top: [0, -1],
          right: [1, 0],
          bottom: [0, 1],
          left: [-1, 0],
        };
        const [dx, dy] = directions[edge];
        return {
          x:
            rect.left -
            bounds.left +
            (edge === "left"
              ? 1
              : edge === "right"
                ? rect.width - 1
                : rect.width * fraction),
          y:
            rect.top -
            bounds.top +
            (edge === "top"
              ? 1
              : edge === "bottom"
                ? rect.height - 1
                : rect.height * fraction),
          dx,
          dy,
        };
      };
      setJoins(
        compact
          ? [
              {
                id: "inputs",
                d: fluidJoin(
                  anchor("inputs", "bottom"),
                  anchor("hub", "top"),
                  26,
                ),
              },
              {
                id: "output",
                d: fluidJoin(
                  anchor("hub", "bottom"),
                  anchor("output", "top"),
                  26,
                ),
              },
            ]
          : [
              ...inputs.map((input, index) => ({
                id: input.id,
                d: fluidJoin(
                  anchor(input.id, "right"),
                  anchor("hub", "left", 0.22 + index * 0.28),
                  26,
                ),
              })),
              {
                id: "output",
                d: fluidJoin(
                  anchor("hub", "right"),
                  anchor("output", "left"),
                  26,
                ),
              },
            ],
      );
    };
    const schedule = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    const observer = new ResizeObserver(schedule);
    observer.observe(element);
    element
      .querySelectorAll<HTMLElement>("[data-node]")
      .forEach((node) => observer.observe(node));
    window.addEventListener("resize", schedule);
    measure();
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener("resize", schedule);
    };
  }, []);

  return (
    <div className={styles.ideaInteractive}>
      <div className={styles.ideaBoard} ref={board}>
        <svg
          className={styles.ideaJoins}
          width="100%"
          height="100%"
          aria-hidden="true"
        >
          {joins.map((join) => (
            <path key={join.id} d={join.d} data-connection={join.id} />
          ))}
        </svg>

        <div
          className={styles.ideaInputs}
          data-node="inputs"
          role="group"
          aria-label="Explore what you connect"
        >
          {inputs.map((input, index) => (
            <button
              type="button"
              key={input.id}
              data-node={input.id}
              className={styles.inputCard}
              onClick={() => setActive(index)}
              aria-pressed={active === index}
              aria-controls="idea-output"
            >
              <span className={styles.inputText}>
                <strong>{input.title}</strong>
                <span>{input.detail}</span>
              </span>
              <img
                className={styles.inputArtwork}
                src={"/images/design-lab/linework/" + input.image + ".svg"}
                width={800}
                height={800}
                loading="lazy"
                alt=""
              />
              <span className={styles.inputSelection} aria-hidden="true">
                <ArrowRight size={17} />
              </span>
            </button>
          ))}
        </div>

        <div className={styles.ideaHub} data-node="hub">
          <div className={styles.hubBrand}>
            <MindwireMark />
            <span>mindwire</span>
          </div>
          <p>Choose your agent.</p>
          <div
            className={styles.agentPicker}
            role="group"
            aria-label="Choose your agent"
          >
            {productAgents.map((item) => (
              <button
                type="button"
                key={item.id}
                onClick={() => setAgent(item.id)}
                aria-label={"Choose " + item.name}
                aria-pressed={agent === item.id}
              >
                <AgentLogo agent={item.id} size={25} surface="light" />
              </button>
            ))}
          </div>
          <span className={styles.chosenAgent}>{chosenAgent.name}</span>
        </div>

        <div className={styles.ideaOutput} id="idea-output" data-node="output">
          <div className={styles.outputHeading}>
            <h3>Inside your product.</h3>
            <p>
              Answers and actions, in the experience your users already know.
            </p>
          </div>
          <div className={styles.outputIllustration}>
            <img
              src="/images/design-lab/linework/product-frame.svg"
              width={800}
              height={800}
              alt="An illustrated product interface with an integrated agent panel, drawn in fine lines with soft sage and lilac accents."
              loading="lazy"
            />
          </div>
          <div className={styles.outputFoot}>
            <span className={styles.outputStatus} aria-live="polite">
              <span>{selectedInput.output}</span>
              <span>With {chosenAgent.name}</span>
            </span>
            <Link
              href="/docs/guides/react"
              aria-label="Build an agent into your interface"
            >
              <ArrowUpRight size={20} />
            </Link>
          </div>
        </div>
      </div>
      <div className={styles.ideaFoot}>
        <p role="status">{selectedInput.explanation}</p>
        <Link href="/docs/concepts/architecture">
          Inside the agent layer <ArrowRight size={18} />
        </Link>
      </div>
    </div>
  );
}
