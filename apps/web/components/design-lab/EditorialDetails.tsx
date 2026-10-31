"use client";
import { useState } from "react";
import { ArrowUpRight, Minus, Plus } from "lucide-react";
import styles from "./studies.module.css";

const details = [
  { title: "Your identity.", label: "01 / EXPRESSION", text: "Your interface. Your voice. Your standards. Put a capable agent inside the product your users already know, with the experience shaped entirely by you.", tags: ["Your brand", "Your interface", "Your voice"] },
  { title: "Your knowledge.", label: "02 / CONTEXT", text: "Bring your files, instructions, and product context into the conversation. Give the agent the understanding it needs to make its work useful.", tags: ["Files & documents", "Product context", "Instructions"] },
  { title: "Your possibilities.", label: "03 / ACTION", text: "Connect the tools that make your product powerful. Let people move from asking a question to getting something done, with you defining the boundaries.", tags: ["Connected tools", "Agent workflows", "Your controls"] },
];
export default function EditorialDetails() {
  const [open, setOpen] = useState(0);
  return <div className={styles.editorialDetails}>{details.map((item, index) => <article key={item.title} className={open === index ? styles.editorialDetailOpen : ""}>
    <button aria-expanded={open === index} aria-controls={`editorial-detail-${index}`} onClick={() => setOpen(open === index ? -1 : index)}><span className={styles.editorialDetailLabel}>{item.label}</span><span className={styles.editorialDetailTitle}>{item.title}</span>{open === index ? <Minus/> : <Plus/>}</button>
    <div id={`editorial-detail-${index}`} hidden={open !== index} className={styles.editorialDetailBody}><div className={styles.editorialStamp} aria-hidden="true"><ArrowUpRight strokeWidth={.7}/></div><div><p>{item.text}</p><div className={styles.editorialTags}>{item.tags.map(tag => <span key={tag}>{tag}</span>)}</div></div></div>
  </article>)}</div>;
}
