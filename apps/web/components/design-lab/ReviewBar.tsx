"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import styles from "./review.module.css";

const studies = [{slug:"fluid",label:"01 / Fluid"},{slug:"editorial",label:"02 / Editorial"},{slug:"electronic",label:"03 / Electronic"}];
export default function ReviewBar() {
  const pathname=usePathname();
  if (pathname === "/design-lab" || pathname?.startsWith("/design-lab/fluid")) return null;
  return <aside className={styles.review} aria-label="Design preview navigation"><Link href="/design-lab" className={styles.title}>MINDWIRE / DESIGN STUDIES</Link><nav aria-label="Switch design direction">{studies.map(study=><Link key={study.slug} href={`/design-lab/${study.slug}`} aria-current={pathname?.includes(study.slug)?"page":undefined}>{study.label}</Link>)}</nav><Link href="/" className={styles.home}>Existing homepage ↗</Link></aside>;
}
