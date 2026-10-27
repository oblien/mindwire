import { Terminal } from "lucide-react";
import CopyButton from "./CopyButton";
import styles from "./product.module.css";

export default function InstallBlock({
  sdk = false,
  pairing = false,
  compact = false,
}: {
  sdk?: boolean;
  pairing?: boolean;
  compact?: boolean;
}) {
  const command = sdk
    ? "npm install mindwire"
    : `npm install -g mindwire\nmindwire connect${pairing ? " pair" : ""}`;
  if (compact)
    return (
      <div className={styles.installInline}>
        <Terminal size={16} aria-hidden="true" />
        <code>{command}</code>
        <CopyButton
          value={command}
          label={
            sdk
              ? "Copy SDK install command"
              : "Copy install and connect commands"
          }
        />
      </div>
    );
  return (
    <div className={styles.installBlock}>
      <div>
        <span>
          <Terminal size={15} /> {sdk ? "Install the SDK" : "On your computer"}
        </span>
        <CopyButton
          value={command}
          label={
            sdk
              ? "Copy SDK install command"
              : "Copy install and connect commands"
          }
        />
      </div>
      <pre>
        <code>{command}</code>
      </pre>
    </div>
  );
}
