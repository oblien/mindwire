import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import styles from "./studies.module.css";
export default function StudyNav({variant}:{variant:"fluid"|"editorial"|"electronic"}){
 return <header className={`${styles.nav} ${styles[`${variant}Nav`]}`}><Link href="/design-lab" className={styles.wordmark}>mindwire<span>{variant==='electronic'?'_':'®'}</span></Link><nav aria-label="Concept navigation"><a href="#concept">The idea</a><a href="#inside">How it works</a><Link href="/docs">Docs</Link></nav><Link href="/docs/guides/quickstart" className={styles.navCta}>{variant==='electronic'?'Initialize':'Start building'}<ArrowUpRight size={15}/></Link></header>;
}
