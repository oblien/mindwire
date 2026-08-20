function Plus() {
  return (
    <svg width="24" height="24" viewBox="0 0 24 24" className="block">
      <path d="M12 0v24M0 12h24" stroke="currentColor" strokeWidth="1.25" />
    </svg>
  );
}

// Technical crosshair marks straddling the corners of a bento block. Solid shade (not alpha)
// so it doesn't compound with the border it sits over.
export default function Corners() {
  return (
    <div aria-hidden className="pointer-events-none absolute inset-0 z-20 text-neutral-300 dark:text-neutral-600">
      <span className="absolute -left-3 -top-3"><Plus /></span>
      <span className="absolute -right-3 -top-3"><Plus /></span>
      <span className="absolute -bottom-3 -left-3"><Plus /></span>
      <span className="absolute -bottom-3 -right-3"><Plus /></span>
    </div>
  );
}
