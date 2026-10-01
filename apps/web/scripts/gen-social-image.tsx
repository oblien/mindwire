import { ImageResponse } from "next/og";
import { writeFile } from "node:fs/promises";


export const alt =
  "Mindwire — Your coding agents, anywhere. Chat, review code, and switch workspaces from your iPhone.";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

function renderSocialImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          alignItems: "center",
          background: "#f8f7f5",
          padding: "64px 78px",
          color: "#202024",
        }}
      >
        <div style={{ display: "flex", flexDirection: "column", width: 705 }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 11,
              fontSize: 28,
              fontWeight: 600,
              marginBottom: 48,
            }}
          >
            <div
              style={{
                width: 29,
                height: 29,
                border: "3px solid #202024",
                borderRadius: 8,
              }}
            />
            Mindwire
          </div>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              fontSize: 77,
              letterSpacing: -4,
              lineHeight: 1.08,
              fontWeight: 600,
            }}
          >
            <span>Keep building.</span>
            <span style={{ color: "#797980" }}>From anywhere.</span>
          </div>
          <div style={{ marginTop: 25, fontSize: 22, color: "#62626b" }}>
            Your favorite coding agents, on iPhone.
          </div>
          <div
            style={{
              display: "flex",
              gap: 23,
              marginTop: 50,
              fontSize: 17,
              color: "#a0442e",
            }}
          >
            <span>Open source</span>
            <span>mindwire.sh</span>
          </div>
        </div>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            background: "#151518",
            color: "#ededf2",
            width: 280,
            height: 512,
            padding: 22,
            borderRadius: 39,
            border: "6px solid #39393e",
          }}
        >
          <div
            style={{
              width: 77,
              height: 18,
              background: "#020203",
              alignSelf: "center",
              borderRadius: 20,
            }}
          />
          <div style={{ marginTop: 26, fontSize: 20, fontWeight: 600 }}>
            Dark mode
          </div>
          <div style={{ marginTop: 8, fontSize: 13, color: "#aaaab4" }}>
            Codex · atlas
          </div>
          <div
            style={{
              display: "flex",
              marginTop: 32,
              marginLeft: 24,
              padding: "14px 17px",
              borderRadius: "16px 16px 4px 16px",
              background: "#343438",
              fontSize: 16,
              lineHeight: 1.5,
            }}
          >
            Add a dark mode toggle.
          </div>
          <div
            style={{
              marginTop: 25,
              fontSize: 16,
              lineHeight: 1.6,
              color: "#d1d1db",
            }}
          >
            I’ll use the existing theme and remember your selection.
          </div>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              marginTop: 22,
              fontSize: 14,
            }}
          >
            <span style={{ color: "#aaaab4" }}>theme.tsx</span>
            <span style={{ color: "#b4d773" }}>+18</span>
          </div>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              marginTop: 16,
              fontSize: 14,
            }}
          >
            <span style={{ color: "#aaaab4" }}>settings.tsx</span>
            <span style={{ color: "#b4d773" }}>+9</span>
          </div>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              marginTop: 27,
              padding: "13px 16px",
              background: "#c2472b",
              borderRadius: 11,
              fontSize: 15,
              color: "#fff",
            }}
          >
            Ready to review
          </div>
          <div
            style={{
              width: 82,
              height: 4,
              alignSelf: "center",
              marginTop: "auto",
              borderRadius: 4,
              background: "#dedee5",
            }}
          />
        </div>
      </div>
    ),
    size,
  );
}

// This artwork is static. Generate it explicitly instead of rendering an image
// for every share request or adding a runtime dependency to the website.
const response = renderSocialImage();
await writeFile(
  new URL("../app/opengraph-image.png", import.meta.url),
  new Uint8Array(await response.arrayBuffer()),
);
await writeFile(
  new URL("../app/opengraph-image.alt.txt", import.meta.url),
  `${alt}\n`,
);
console.log("Generated app/opengraph-image.png and its alt text");
