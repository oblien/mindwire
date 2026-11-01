"use client";

import { useEffect, useState, type CSSProperties } from "react";
import SiteNav from "./SiteNav";
import styles from "./scroll-navigation.module.css";

// The original navigation's eight blur bands soften into the page below.
const blurMasks = [
  "linear-gradient(transparent 62.5%, #000 75%, #000 87.5%, transparent 100%)",
  "linear-gradient(transparent 50%, #000 62.5%, #000 75%, transparent 87.5%)",
  "linear-gradient(transparent 37.5%, #000 50%, #000 62.5%, transparent 75%)",
  "linear-gradient(transparent 25%, #000 37.5%, #000 50%, transparent 62.5%)",
  "linear-gradient(transparent 12.5%, #000 25%, #000 37.5%, transparent 50%)",
  "linear-gradient(transparent 0%, #000 12.5%, #000 25%, transparent 37.5%)",
  "linear-gradient(#000 0%, #000 12.5%, transparent 25%)",
  "linear-gradient(#000 0%, transparent 12.5%)",
];

export default function ScrollNavigation() {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    const section = document.getElementById("concept");
    if (!section) return;

    let frame = 0;
    const update = () => {
      frame = 0;
      setVisible(section.getBoundingClientRect().bottom <= 0);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    // Scroll checks also catch anchor jumps that skip the entire section.
    update();
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    const observer = new ResizeObserver(schedule);
    observer.observe(section);
    return () => {
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, []);

  return (
    <div
      className={styles.navigation}
      data-scroll-navigation
      data-visible={visible}
      inert={!visible}
    >
      <div className={styles.blur} aria-hidden="true">
        {blurMasks.map((mask, index) => (
          <span
            key={mask}
            style={
              {
                "--blur": `${2 ** (index - 1)}px`,
                "--mask": mask,
              } as CSSProperties
            }
          />
        ))}
      </div>
      {visible && <SiteNav embedded />}
    </div>
  );
}
