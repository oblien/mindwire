import { Check, Cloud, FileText, Laptop, MoreHorizontal } from "lucide-react";
import BotAvatar from "./BotAvatar";
import { PhoneArt } from "./DeviceArt";
import styles from "./home-details.module.css";

export default function ContinuityScene({ cloud }: { cloud: boolean }) {
  return <div className={styles.continuityScene}>
    <svg className={styles.continuityWires} viewBox="0 0 540 570" fill="none" aria-hidden="true"><path d="M66 264v87q0 26 26 26h151q26 0 26 26v32h68" /><circle cx="66" cy="264" r="4" /><circle cx="337" cy="435" r="4" /></svg>
    <div className={styles.desktop} role="img" aria-label={`Cleo preparing a weekly plan on ${cloud ? "your Cloud Computer" : "your Mac"}`}>
      <div className={styles.desktopBar}><div aria-hidden="true"><i /><i /><i /></div><span>{cloud ? <Cloud size={12} /> : <Laptop size={12} />}{cloud ? "Cloud Computer" : "My Mac"}</span><span className={styles.onlineDot} /></div>
      <div className={styles.desktopBody}>
        <div className={styles.desktopRail}><BotAvatar size={28} /><BotAvatar character="robot" size={28} /><BotAvatar character="sprout" size={28} /></div>
        <div className={styles.desktopConversation}>
          <div className={styles.conversationTitle}><strong>A little room to breathe</strong><MoreHorizontal size={15} /></div>
          <p className={styles.desktopPrompt}>Can you get my week in order?</p>
          <div className={styles.desktopReply}><BotAvatar size={25} /><p>Of course. Let’s leave space<br />for the good stuff, too.</p></div>
          <div className={styles.savedFile}><FileText size={20} /><span>weekly-plan.md<small>Ready when you are</small></span><Check size={14} /></div>
        </div>
      </div>
      <div className={styles.desktopBottom}><Check size={11} />Saved in your workspace</div>
    </div>
    <div className={styles.handoffNote}><span className={styles.handoffLine} /><strong>Heading out?</strong><span>Take the conversation<br />with you.</span></div>
    <div className={styles.continuityPhone}><PhoneArt cloud={cloud} /></div>
    <div className={styles.continuityCaption}><span className={styles.onlineDot} />Same conversation. Same workspace. Another screen.</div>
  </div>;
}
