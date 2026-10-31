/** Shared geometry for Mindwire's original vector posters and moving signal fields. */
export type SignalVariant = "bloom" | "cobalt" | "thermal" | "lime" | "closing";

export const signalPalettes: Record<SignalVariant, { background: string; colors: string[] }> = {
  bloom: { background: "#e8e8de", colors: ["#252c29", "#7187cd", "#afcaba", "#ee8866", "#f5cf91"] },
  cobalt: { background: "#151e33", colors: ["#c6dccd", "#7897e3", "#4c64af", "#ec9d7e", "#e8e5d8"] },
  thermal: { background: "#322c29", colors: ["#eca27f", "#f5d59e", "#baceb4", "#657cbd", "#ece7d7"] },
  lime: { background: "#dbe2cb", colors: ["#394a3e", "#8ba989", "#f1e9d5", "#de8c6f", "#6d82bd"] },
  closing: { background: "#141e1c", colors: ["#36554c", "#7b9f96", "#d3dbc8", "#df9e7a", "#677fc1"] },
};

/** Open-ended ribbons sweep around a quiet center reserved for the closing headline. */
export function signalBand(index: number, seconds = 0) {
  const k = index - 16;
  const drift = Math.sin(seconds * .16) * 20;
  const x = k * 25;
  const left = -80 + x;
  const y = 490 + k * 5;
  return `M${left} ${y}C${210 + x} ${-240 + k * 5 + drift} ${360 + x * .12} ${720 + k * 5} ${680 + x * .15} ${325 + k * 9}S${1040 + x * .6} ${-100 + k * 7 + drift} ${1450 + x} ${310 + k * 7}`;
}

export function signalPoster(variant: SignalVariant) {
  const { background, colors } = signalPalettes[variant];
  return `<svg xmlns="http://www.w3.org/2000/svg" width="1360" height="684" viewBox="0 0 1360 684" fill="none"><title>Mindwire woven signal field</title><rect width="1360" height="684" fill="${background}"/>${Array.from({length: 34}, (_, i) => `<path d="${signalBand(i)}" stroke="${colors[Math.floor(i / 7) % colors.length]}" stroke-width="15"/>`).join("")}</svg>\n`;
}
