export default function OblienLogo({ size = 30, className = "" }: { size?: number; className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={`logo-mask ${className}`}
      style={
        {
          ["--logo"]: "url(/oblien.svg)",
          width: size,
          height: size,
          // The source SVG has heavy padding inside its viewBox; zoom the mask so the mark
          // fills its box (and its left edge lines up with the content).
          maskSize: "172%",
          WebkitMaskSize: "172%",
        } as React.CSSProperties
      }
    />
  );
}
