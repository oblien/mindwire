export type SiteIllustrationKind =
  "cloud" | "devices" | "code" | "context" | "identity" | "product";

export default function SiteIllustration({
  kind,
}: {
  kind: SiteIllustrationKind;
}) {
  if (kind !== "cloud" && kind !== "devices") {
    const files = {
      code: "sdk-embed",
      context: "context-sources",
      identity: "identity-system",
      product: "product-frame",
    };
    return (
      <img
        src={"/images/design-lab/linework/" + files[kind] + ".svg"}
        alt=""
        width={400}
        height={400}
        loading="lazy"
      />
    );
  }
  return (
    <svg
      viewBox="0 0 400 400"
      fill="none"
      stroke="#30372c"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {kind === "cloud" ? (
        <>
          <path
            d="M72 186V292Q72 317 98 317H311M302 80V117"
            strokeDasharray="3 6"
            opacity=".5"
          />
          <rect
            x="109"
            y="168"
            width="218"
            height="124"
            rx="20"
            fill="#e3d8e9"
          />
          <rect
            x="84"
            y="199"
            width="218"
            height="124"
            rx="20"
            fill="#f8f7f3"
          />
          <path d="M85 242H301M85 282H301" />
          <circle cx="108" cy="222" r="4" fill="#cedcba" />
          <circle cx="108" cy="263" r="4" fill="#decae6" />
          <circle cx="108" cy="303" r="4" fill="#edc7aa" />
          <path
            d="M128 222H173M128 263H163M128 303H183M254 222H280M254 263H280M254 303H280"
            opacity=".6"
          />
          <path
            d="M115 161C89 161 81 125 102 112C103 72 157 55 183 86C205 75 229 92 231 113C263 112 273 159 240 165H128"
            fill="#e3ead7"
          />
          <path d="M132 135L154 113L176 135M154 115V185" />
          <circle cx="154" cy="186" r="5" fill="#f8f7f3" />
          <path d="M51 88H73M62 77V99M324 343H346M335 332V354" opacity=".45" />
          <circle cx="302" cy="80" r="4" fill="#30372c" stroke="none" />
        </>
      ) : (
        <>
          <path
            d="M47 315H289Q321 315 321 284V92"
            strokeDasharray="3 6"
            opacity=".5"
          />
          <rect x="47" y="89" width="254" height="182" rx="15" fill="#f8f7f3" />
          <path d="M47 119H301M149 272V293M116 293H184" />
          <circle cx="63" cy="104" r="2" fill="#30372c" />
          <circle cx="74" cy="104" r="2" />
          <circle cx="85" cy="104" r="2" />
          <rect x="67" y="138" width="142" height="72" rx="9" fill="#e3ead7" />
          <path
            d="M84 156H175M84 170H192M84 184H157M68 231H193M68 243H173"
            opacity=".6"
          />
          <rect
            x="227"
            y="153"
            width="115"
            height="189"
            rx="23"
            fill="#e3d8e9"
          />
          <path d="M267 167H302" strokeWidth="3" />
          <rect x="241" y="196" width="87" height="50" rx="11" fill="#f8f7f3" />
          <path
            d="M255 211H314M255 224H297M249 267H313M249 279H302M276 328H296"
            opacity=".6"
          />
          <circle cx="47" cy="315" r="4" fill="#30372c" stroke="none" />
          <circle cx="321" cy="92" r="5" fill="#f8f7f3" />
          <path d="M311 55H335M323 43V67M84 345H106M95 334V356" opacity=".45" />
        </>
      )}
    </svg>
  );
}
