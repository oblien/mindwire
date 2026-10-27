import { useId } from "react";

/** A connected artwork from Home, kept decorative so the account stays the focus. */
export function AccountArtwork({ className }: { className?: string }) {
  const clipId = useId();
  const window = "M104 18H356Q376 18 376 38Q376 58 396 58Q416 58 416 78V166Q416 186 396 186H64Q44 186 44 166V78Q44 58 64 58Q84 58 84 38Q84 18 104 18Z";
  return (
    <svg className={className} viewBox="0 0 460 290" fill="none" aria-hidden="true">
      <defs><clipPath id={clipId}><path d={window} /></clipPath></defs>
      <image href="/images/framework/cobalt.webp" x="44" y="18" width="372" height="168" preserveAspectRatio="xMidYMid slice" clipPath={`url(#${clipId})`} />
      <path d="M230 186v23q0 17 17 17h28q17 0 17 17v6" stroke="var(--mw-ink)" strokeWidth="2" />
      <rect x="121" y="243" width="260" height="44" rx="22" fill="var(--mw-ink)" />
      <text x="251" y="272" textAnchor="middle" fill="var(--mw-on-ink)" fontFamily="Mindwire Host, sans-serif" fontSize="22">Pick up anywhere.</text>
    </svg>
  );
}
