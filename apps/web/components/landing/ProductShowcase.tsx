"use client";

import { useEffect, useId, useState } from "react";
import { BookOpen, Layers2 } from "lucide-react";
import ProductConversation from "./ProductConversation";
import AgentEngine from "./AgentEngine";
import { navigateTabs } from "./tabs";
import styles from "./product-showcase.module.css";

const examples = [
  { id: "course", label: "Learning app", icon: BookOpen, description: "A conversation becomes a playable course inside a learning app. Ask for a quiz, and it becomes part of the course." },
  { id: "launch", label: "Creative workspace", icon: Layers2, description: "A brief becomes a website, campaign artwork, and a launch plan. Ready to use, right inside your creative workspace." },
] as const;
type Example = (typeof examples)[number]["id"];

export default function ProductShowcase() {
  const id = useId();
  const [selected, setSelected] = useState<Example>("course");

  useEffect(() => {
    const syncHash = () => {
      if (window.location.hash === "#in-action") setSelected("launch");
      else if (window.location.hash === "#agent-in-your-product") setSelected("course");
    };
    syncHash();
    window.addEventListener("hashchange", syncHash);
    return () => window.removeEventListener("hashchange", syncHash);
  }, []);

  const select = (example: Example) => {
    setSelected(example);
    window.history.replaceState(window.history.state, "", example === "course" ? "#agent-in-your-product" : "#in-action");
  };

  return <section className={styles.section} id="agent-in-your-product" aria-labelledby="conversation-title">
    <span id="in-action" className={styles.anchor} aria-hidden />
    <div className={styles.intro}>
      <h2 id="conversation-title">An idea in chat.<br /><em>Made real in your product.</em></h2>
      <p>{examples.find(example => example.id === selected)!.description}</p>
    </div>
    <div className={styles.tabs} role="tablist" aria-label="Product use cases" onKeyDown={navigateTabs}>
      {examples.map(example => <button key={example.id} type="button" role="tab" id={id + "-tab-" + example.id} aria-controls={id + "-panel-" + example.id} aria-selected={selected === example.id} tabIndex={selected === example.id ? 0 : -1} onClick={() => select(example.id)}><example.icon size={18} aria-hidden />{example.label}</button>)}
    </div>
    <div className={styles.panel} role="tabpanel" id={id + "-panel-course"} aria-labelledby={id + "-tab-course"} hidden={selected !== "course"}>
      <ProductConversation active={selected === "course"} />
    </div>
    <div className={styles.panel} role="tabpanel" id={id + "-panel-launch"} aria-labelledby={id + "-tab-launch"} hidden={selected !== "launch"}>
      <AgentEngine />
    </div>
  </section>;
}
