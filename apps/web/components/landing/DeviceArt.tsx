import { ArrowUp, Check, ChevronLeft, Folder, MoreHorizontal, Plus } from "lucide-react";
import BotAvatar from "./BotAvatar";
import styles from "./product.module.css";

export function PhoneArt({ cloud = false }: { cloud?: boolean }) {
  return <div className={styles.phoneArt} role="img" aria-label={`Illustrative Mindwire phone conversation with Cleo, connected to ${cloud ? "a Cloud Computer" : "your Mac"}`}>
    <div className={styles.phoneStatus}><span>9:41</span><i /><span>▰</span></div>
    <div className={styles.phoneTop}><ChevronLeft size={17} /><BotAvatar size={30} /><div>Cleo<small>{cloud ? "Cloud Computer" : "My Mac"}</small></div><MoreHorizontal size={18} /></div>
    <div className={styles.phoneMessage}>Can you get my week in order?</div>
    <div className={styles.phoneReply}><BotAvatar size={27} /><p>A little structure,<br />a little breathing room.</p></div>
    <div className={styles.phoneDocument}><span>YOUR WEEK, SORTED</span><strong>Space for the good stuff.</strong><p><Check size={12} /> Prepare Monday’s meeting</p><p><Check size={12} /> Finish the reading tracker</p><p><Check size={12} /> Leave Friday afternoon open</p><div><Folder size={13} /> weekly-plan.md <Check size={12} /></div></div>
    <div className={styles.phoneComposer}><Plus size={14} /><span>What’s next?</span><ArrowUp size={17} /></div>
    <div className={styles.phoneHome} />
  </div>;
}

export function MacArt() {
  return <div className={styles.macArt} role="img" aria-label="Illustrative Mindwire desktop with Cleo, Kit and Sage, and a completed reading tracker">
    <div className={styles.macScreen}>
      <div className={styles.macTitle}><span><i /><i /><i /></span><span>My workspace</span><MoreHorizontal size={13} /></div>
      <div className={styles.macBody}>
        <div className={styles.macSidebar}>{(["bean", "robot", "sprout"] as const).map((character, i) => <div key={character} data-selected={i === 1}><BotAvatar character={character} size={27} /><span>{["Cleo", "Kit", "Sage"][i]}</span></div>)}</div>
        <div className={styles.macChat}><span>Let’s build a reading tracker.</span><div><BotAvatar character="robot" size={26} /><p>One small app.<br />A lot more good books.</p></div><div className={styles.miniApp}><span>READING CORNER</span><strong>Your next chapter.</strong><div><i /><i /><i /></div><p><Check size={10} /> Your app is ready</p></div></div>
      </div>
    </div><div className={styles.macBase} />
  </div>;
}
