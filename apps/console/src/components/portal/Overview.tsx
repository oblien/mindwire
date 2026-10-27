import { Link } from "react-router-dom";
import { ArrowRight, BookOpen, Download, HardDrive, Monitor, Plus, CreditCard, Smartphone } from "lucide-react";
import { useCloud } from "@/lib/cloud";
import type { AuthUser } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { AccountArtwork } from "@/components/AccountArtwork";
import { ComputerRow } from "../CloudComputers";
import { Loading, PageTitle, money } from "./common";
import styles from "./portal.module.css";

export function Overview({ user }: { user: AuthUser }) {
  const cloud = useCloud();
  const allocated = cloud.computers.filter(c => c.workspaceId && c.state !== "deleting");
  const hasComputers = cloud.computers.length > 0;
  return <div className="space-y-8">
    <PageTitle title={`Good to see you${user.name ? `, ${user.name.split(" ")[0]}` : ""}.`} description="Your personal workspace. Computers, devices, and the projects you’re working on." action={hasComputers ? <Button asChild><Link to="/computers/new"><Plus />New computer</Link></Button> : undefined} />
    {!cloud.loaded && !cloud.error && <Loading />}
    {cloud.loaded && !hasComputers && <section className={styles.intro}>
      <div><h2>Your work. Room to grow.</h2><p>Use your own computer for free, or add a personal Cloud Computer that stays online. Pick up your projects from the Mindwire app.</p><div className="flex flex-wrap gap-3"><Button asChild><Link to="/computers/new">Explore Cloud Computers<ArrowRight /></Link></Button><Button asChild variant="ghost"><a href="https://mindwire.sh/app#download">Get the app</a></Button></div></div>
      <AccountArtwork className={styles.introArt} />
    </section>}
    {hasComputers && <>
      <div className="grid gap-4 sm:grid-cols-3">
        {[
          { icon: Monitor, label: "Cloud Computers", value: String(allocated.length), detail: "In your account", tone: "blue" },
          { icon: HardDrive, label: "Included storage", value: `${allocated.reduce((n,c) => n+c.plan.diskGb,0)} GB`, detail: "Across your computers", tone: "mint" },
          { icon: CreditCard, label: "Monthly plans", value: money(allocated.reduce((n,c) => n+c.plan.monthlyCents,0)), detail: "Total per month", tone: "peach" },
        ].map(item => <Card key={item.label}><CardContent className={styles.stat}><div className={styles.statLabel}><span style={{ background: `var(--mw-${item.tone}-soft)`, color: `var(--mw-${item.tone})` }} className="grid size-9 place-items-center rounded-xl"><item.icon className="size-4" /></span>{item.label}</div><p className={styles.statValue}>{item.value}</p><p className={styles.statDetail}>{item.detail}</p></CardContent></Card>)}
      </div>
      <section className="space-y-4"><div className="flex items-center justify-between gap-4"><h2 className="text-base font-medium">Your computers</h2><Button asChild size="sm" variant="ghost"><Link to="/computers">View all<ArrowRight /></Link></Button></div><Card className="overflow-hidden"><CardContent className="p-0">{cloud.computers.slice(0,4).map(c => <ComputerRow key={c.id} computer={c} />)}</CardContent></Card></section>
    </>}
    <section className="space-y-4" aria-label="Account shortcuts">
      <h2 className="text-base font-medium">Make yourself at home</h2>
      <div className={styles.quickLinks}>
        <a href="https://mindwire.sh/app#download" className={styles.quickLink}><span><Download className="size-5" /></span><h3>Get Mindwire<ArrowRight className="size-4" /></h3><p>Create, review, and keep working from your Mac or phone.</p></a>
        <Link to="/devices" className={styles.quickLink}><span><Smartphone className="size-5" /></span><h3>Your connections<ArrowRight className="size-4" /></h3><p>See where you’re signed in and manage your devices.</p></Link>
        <a href="https://mindwire.sh/docs" className={styles.quickLink}><span><BookOpen className="size-5" /></span><h3>A little guidance<ArrowRight className="size-4" /></h3><p>Connect a computer and start your first project in Mindwire.</p></a>
      </div>
    </section>
  </div>;
}
