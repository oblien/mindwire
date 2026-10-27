import { ImageResponse } from "next/og";
import { writeFile } from "node:fs/promises";

export const alt = "Mindwire — Your product. Powered by agents. White-label leading agents without rebuilding the agent layer.";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

function renderSocialImage() {
  return new ImageResponse(
    <div style={{ width: "100%", height: "100%", display: "flex", alignItems: "center", background: "#10110f", padding: "64px 70px", color: "#f1f1e9", gap: 45 }}>
      <div style={{ display: "flex", flexDirection: "column", width: 650 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12, fontSize: 27, fontWeight: 600, marginBottom: 50 }}>
          <div style={{ width: 27, height: 27, border: "3px solid #f1f1e9", borderRadius: 7 }} />Mindwire
        </div>
        <div style={{ display: "flex", flexDirection: "column", fontSize: 73, letterSpacing: -4, lineHeight: 1.08, fontWeight: 600 }}>
          <span>Your product.</span><span style={{ color: "#ecaa7d" }}>Powered by agents.</span>
        </div>
        <div style={{ marginTop: 26, fontSize: 22, lineHeight: 1.5, color: "#a3a69b", maxWidth: 580 }}>White-label leading agents. Add advanced AI to your product without rebuilding the agent layer.</div>
        <div style={{ display: "flex", gap: 24, marginTop: 48, fontSize: 16, color: "#8abdae" }}><span>TypeScript · Go · REST</span><span>mindwire.sh</span></div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", width: 350, padding: 26, borderRadius: 24, background: "#1d231f", color: "#ebeee7" }}>
        <div style={{ fontSize: 12, color: "#aebb9f", marginBottom: 25 }}>The agent layer is already built.</div>
        <div style={{ fontSize: 22, marginBottom: 10 }}>Make it your product.</div>
        <div style={{ fontSize: 15, lineHeight: 1.5, color: "#c8cdc3" }}>Native agent capabilities. Your tools, knowledge, mindset, and goals.</div>
        <div style={{ display: "flex", flexDirection: "column", marginTop: 28, padding: 19, borderRadius: 13, background: "#2b352d", gap: 14 }}>
          <span style={{ fontSize: 18, color: "#e9c395" }}>Mindwire SDK & runtime</span>
          <span style={{ fontSize: 13 }}>Custom tools & knowledge</span><span style={{ fontSize: 13 }}>Instructions & goals</span><span style={{ fontSize: 13 }}>Automation & approvals</span>
        </div>
        <div style={{ fontSize: 13, marginTop: 25, color: "#aebb9f" }}>Codex · Claude Code · OpenCode</div>
      </div>
    </div>, size,
  );
}

// Static artwork generated from the same product positioning as the website.
const response = renderSocialImage();
await writeFile(new URL("../app/opengraph-image.png", import.meta.url), new Uint8Array(await response.arrayBuffer()));
await writeFile(new URL("../app/opengraph-image.alt.txt", import.meta.url), `${alt}\n`);
console.log("Generated app/opengraph-image.png and its alt text");
