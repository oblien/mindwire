import {
  ArrowUp,
  Check,
  Cloud,
  FileText,
  Laptop,
  Smartphone,
} from "lucide-react";
import styles from "./home-details.module.css";

export default function ContinuityScene({ cloud }: { cloud: boolean }) {
  return (
    <div
      className={styles.continuityScene}
      aria-label="The same project on your computer and phone"
    >
      <div className={styles.desktop}>
        <div className={styles.desktopBar}>
          {cloud ? <Cloud size={18} /> : <Laptop size={18} />}
          <span>{cloud ? "Cloud Computer" : "My Mac"}</span>
          <Check size={16} />
        </div>
        <div className={styles.desktopConversation}>
          <p className={styles.documentLabel}>Weekly plan</p>
          <h3>
            Make space for
            <br />
            the work that matters.
          </h3>
          <p className={styles.desktopPrompt}>
            Plan my week around these priorities.
          </p>
          <div className={styles.savedFile}>
            <FileText size={22} />
            <span>
              weekly-plan.md<small>Saved in your workspace</small>
            </span>
            <Check size={16} />
          </div>
        </div>
      </div>
      <div className={styles.continuityPhone}>
        <div className={styles.phoneHeader}>
          <Smartphone size={18} />
          <span>Mindwire</span>
        </div>
        <p>
          Same project.
          <br />
          <strong>Next conversation.</strong>
        </p>
        <div className={styles.phoneFile}>
          <FileText size={20} />
          <span>
            Weekly plan<small>Ready to review</small>
          </span>
        </div>
        <div className={styles.phoneComposer}>
          <span>Move the review to Friday.</span>
          <ArrowUp size={18} />
        </div>
      </div>
      <p className={styles.continuityCaption}>
        Your files and context come with you.
      </p>
    </div>
  );
}
