import { Plus } from "@/components/site/InterfaceIcon";
import styles from "./product.module.css";

export default function Questions({ items }: { items: { question: string; answer: React.ReactNode }[] }) {
  return <div className={styles.questions}>{items.map(item => <details key={item.question}>
    <summary>{item.question}<Plus size={19} /></summary>
    <div>{item.answer}</div>
  </details>)}</div>;
}
