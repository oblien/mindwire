import { ImageResponse } from "next/og";
import { readFile } from "node:fs/promises";
import { join } from "node:path";

export const socialImageSize = { width: 1200, height: 630 };

type SocialImageOptions = {
  title: string;
  description: string;
  label: string;
  hero?: boolean;
  artwork?: string;
  agents?: { name: string; image: string }[];
};

const fonts = Promise.all([
  readFile(join(process.cwd(), "assets/social/host-medium.ttf")),
  readFile(join(process.cwd(), "assets/social/geist-regular.ttf")),
]);
const defaultArtwork = readFile(
  join(process.cwd(), "public/images/design-lab/linework/context-sources.svg"),
  "base64",
);

function Wordmark() {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
      <svg width="37" height="32" viewBox="0 0 120 95" fill="none">
        <path
          d="M10 82V36C10 4 53 4 53 36V82M53 36C53 4 96 4 96 36V82H113"
          stroke="#252622"
          strokeWidth="17"
          strokeLinejoin="round"
        />
      </svg>
      <span
        style={{
          fontFamily: "Host",
          fontSize: 38,
          fontWeight: 500,
          letterSpacing: -2,
        }}
      >
        mindwire
      </span>
    </div>
  );
}

export async function renderSocialImage({
  title,
  description,
  label,
  hero = false,
  artwork,
  agents = [],
}: SocialImageOptions) {
  const [host, geist] = await fonts;
  const graphic =
    artwork ?? `data:image/svg+xml;base64,${await defaultArtwork}`;
  const texture = hero
    ? `data:image/jpeg;base64,${await readFile(join(process.cwd(), "assets/social/hero-optical.jpg"), "base64")}`
    : undefined;
  const titleSize =
    title.length > 100
      ? 46
      : title.length > 65
        ? 56
        : title.length > 40
          ? 66
          : 78;
  const descriptionText =
    description.length > 175
      ? description.slice(0, 172).replace(/\s+\S*$/, "") + "…"
      : description;
  return new ImageResponse(
    <div
      style={{
        width: "100%",
        height: "100%",
        display: "flex",
        padding: 14,
        background: "#e5e4df",
        fontFamily: "Geist",
        color: "#252622",
      }}
    >
      <div
        style={{
          display: "flex",
          position: "relative",
          width: "100%",
          height: "100%",
          background: "#f8f7f3",
          borderRadius: 26,
          overflow: "hidden",
        }}
      >
        {texture && (
          <img
            src={texture}
            width={1172}
            height={586}
            alt=""
            style={{
              position: "absolute",
              left: 0,
              bottom: -155,
              opacity: 0.8,
            }}
          />
        )}
        {texture && (
          <div
            style={{
              display: "flex",
              position: "absolute",
              inset: 0,
              background:
                "linear-gradient(180deg, #f8f7f3 0%, #f8f7f3 50%, rgba(248,247,243,0) 90%)",
            }}
          />
        )}
        <div
          style={{
            position: "absolute",
            top: 37,
            left: 48,
            right: 48,
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
          }}
        >
          <Wordmark />
          <span style={{ fontSize: 17, color: "#62675c" }}>mindwire.sh</span>
        </div>
        {hero ? (
          <div
            style={{
              position: "absolute",
              top: 145,
              left: 35,
              right: 35,
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              textAlign: "center",
            }}
          >
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 29,
                marginBottom: 26,
              }}
            >
              {agents.map((agent) => (
                <img
                  key={agent.name}
                  src={agent.image}
                  width={27}
                  height={27}
                  alt={agent.name}
                />
              ))}
            </div>
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                alignItems: "center",
                fontFamily: "Host",
                fontSize: 88,
                fontWeight: 500,
                lineHeight: 1.02,
                letterSpacing: -5,
              }}
            >
              {title.split("\n").map((line) => (
                <span key={line}>{line}</span>
              ))}
            </div>
            <div
              style={{
                display: "flex",
                maxWidth: 760,
                marginTop: 27,
                fontSize: 22,
                lineHeight: 1.5,
                color: "#62675c",
                whiteSpace: "pre-wrap",
              }}
            >
              {description}
            </div>
          </div>
        ) : (
          <div
            style={{
              display: "flex",
              position: "absolute",
              left: 0,
              top: 0,
              width: "100%",
              height: "100%",
            }}
          >
            <div
              style={{
                position: "absolute",
                top: 144,
                left: 50,
                display: "flex",
                gap: 12,
                alignItems: "center",
                fontSize: 16,
                color: "#725780",
              }}
            >
              <span style={{ width: 20, height: 1, background: "#a18caf" }} />
              {label}
            </div>
            <div
              style={{
                position: "absolute",
                left: 50,
                top: 195,
                width: 745,
                bottom: 100,
                display: "flex",
                flexDirection: "column",
                justifyContent: "center",
              }}
            >
              <div
                style={{
                  display: "flex",
                  fontFamily: "Host",
                  fontSize: titleSize,
                  fontWeight: 500,
                  lineHeight: 1.07,
                  letterSpacing: -titleSize * 0.046,
                  whiteSpace: "pre-wrap",
                  overflowWrap: "anywhere",
                }}
              >
                {title}
              </div>
              <div
                style={{
                  display: "flex",
                  marginTop: 25,
                  maxWidth: 650,
                  fontSize: 21,
                  lineHeight: 1.5,
                  color: "#62675c",
                }}
              >
                {descriptionText}
              </div>
            </div>
            <img
              src={graphic}
              width={285}
              height={285}
              alt=""
              style={{ position: "absolute", right: 23, top: 218 }}
            />
            <div
              style={{
                position: "absolute",
                left: 50,
                bottom: 38,
                right: 50,
                display: "flex",
                justifyContent: "space-between",
                borderTop: "1px solid #d9dbd1",
                paddingTop: 20,
                fontSize: 14,
                color: "#62675c",
              }}
            >
              <span>The agent layer for your product.</span>
              <span>Made to be made yours.</span>
            </div>
          </div>
        )}
      </div>
    </div>,
    {
      ...socialImageSize,
      fonts: [
        { name: "Host", data: host, style: "normal", weight: 500 },
        { name: "Geist", data: geist, style: "normal", weight: 400 },
      ],
    },
  );
}
