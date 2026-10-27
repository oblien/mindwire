import {
  ArrowUp,
  BookOpen,
  Check,
  ChevronLeft,
  FileText,
  Folder,
  MoreHorizontal,
  Plus,
  Terminal,
} from "lucide-react";
import styles from "./product.module.css";

export function PhoneArt({ cloud = false }: { cloud?: boolean }) {
  return (
    <div
      className={styles.phoneArt}
      role="img"
      aria-label={`Mindwire phone example, connected to ${cloud ? "a Cloud Computer" : "your Mac"}`}
    >
      <div className={styles.phoneStatus}>
        <span>9:41</span>
        <i />
        <span>▰</span>
      </div>
      <div className={styles.phoneTop}>
        <ChevronLeft size={17} />
        <div>
          Mindwire<small>{cloud ? "Cloud Computer" : "My Mac"}</small>
        </div>
        <MoreHorizontal size={18} />
      </div>
      <div className={styles.phoneMessage}>
        Plan my week around these priorities.
      </div>
      <div className={styles.phoneReply}>
        <Check size={18} />
        <p>Your plan is ready.</p>
      </div>
      <div className={styles.phoneDocument}>
        <span>Weekly plan</span>
        <strong>A little room to focus.</strong>
        <p>
          <Check size={14} /> Prepare the team review
        </p>
        <p>
          <Check size={14} /> Finish the reading tracker
        </p>
        <div>
          <FileText size={16} /> weekly-plan.md
        </div>
      </div>
      <div className={styles.phoneComposer}>
        <Plus size={16} />
        <span>What’s next?</span>
        <ArrowUp size={18} />
      </div>
      <div className={styles.phoneHome} />
    </div>
  );
}

export function MacArt() {
  const projects = [
    { icon: BookOpen, name: "Reading" },
    { icon: Terminal, name: "Website" },
    { icon: Folder, name: "Projects" },
  ];
  return (
    <div
      className={styles.macArt}
      role="img"
      aria-label="Mindwire desktop example with projects and a completed reading tracker"
    >
      <div className={styles.macScreen}>
        <div className={styles.macTitle}>
          <span>Mindwire</span>
          <span>My workspace</span>
          <MoreHorizontal size={16} />
        </div>
        <div className={styles.macBody}>
          <div className={styles.macSidebar}>
            {projects.map(({ icon: Icon, name }, i) => (
              <div key={name} data-selected={i === 0}>
                <Icon size={22} strokeWidth={1.5} />
                <span>{name}</span>
              </div>
            ))}
          </div>
          <div className={styles.macChat}>
            <span>Build me a reading tracker.</span>
            <div>
              <Check size={18} />
              <p>Built and saved in your workspace.</p>
            </div>
            <div className={styles.miniApp}>
              <span>Reading corner</span>
              <strong>Your next chapter.</strong>
              <div>
                <i />
                <i />
                <i />
              </div>
              <p>
                <Check size={14} /> Ready to use
              </p>
            </div>
          </div>
        </div>
      </div>
      <div className={styles.macBase} />
    </div>
  );
}
