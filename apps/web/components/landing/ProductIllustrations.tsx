import AgentLogo from "../marketing/AgentLogo";
import styles from "./product-home.module.css";

export function ToolsIllustration() {
  return <div className={`${styles.heroIllustration} ${styles.toolsIllustration}`} aria-hidden="true">
    <svg viewBox="0 0 190 220" fill="none">
      <path d="M18 29C91 2 163 38 152 78S18 122 39 171c8 21 45 26 105 20" stroke="var(--mw-line)" strokeWidth="1.3" />
      <g transform="rotate(-14 89 108)"><rect x="31" y="45" width="112" height="134" rx="14" fill="#a7c0e4" stroke="#7197c8" /><path d="M48 65h61M48 75h38" stroke="#6485b3" strokeOpacity=".6" strokeWidth="3" strokeLinecap="round" /></g>
      <g transform="rotate(7 110 128)"><rect x="57" y="66" width="109" height="128" rx="15" fill="#efc5a1" stroke="#d9a57c" /><rect x="72" y="82" width="35" height="35" rx="10" fill="#668bc1" /><path d="m85 94-5 5 5 5m9-10 5 5-5 5" stroke="#f3f6fb" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" /><path d="M72 134h71M72 145h49M72 166h31" stroke="#c68f65" strokeWidth="4" strokeLinecap="round" /><path d="m134 161 4 4 7-8" stroke="#577a59" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" /></g>
    </svg>
    <span>Your tools. Your world.</span>
  </div>;
}

export function AgentsIllustration() {
  return <div className={`${styles.heroIllustration} ${styles.agentsIllustration}`} aria-hidden="true">
    <svg className={styles.illustrationTrail} viewBox="0 0 190 220" fill="none"><path d="M35 22c98-4 123 39 105 74S13 137 42 177c11 16 40 26 104 19" stroke="var(--mw-line)" strokeWidth="1.3" /></svg>
    <div className={styles.illustratedAgent}><AgentLogo agent="codex" size={24} /><span>Codex</span></div>
    <div className={styles.illustratedAgent}><AgentLogo agent="claude" size={24} /><span>Claude Code</span></div>
    <div className={styles.illustratedAgent}><AgentLogo agent="opencode" size={22} surface="light" /><span>OpenCode</span></div>
    <span>Great minds. On your side.</span>
  </div>;
}
