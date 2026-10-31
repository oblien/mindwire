import { signalBand, signalPalettes, type SignalVariant } from "./signal-field";

export type Artwork = SignalVariant;
export type MotionRenderer = {
  resize: (width: number, height: number) => void;
  draw: (seconds: number) => void;
  destroy: () => void;
};

/** The moving artwork uses the same original paths and palette as its SVG poster. */
export async function createArtwork(
  canvas: HTMLCanvasElement,
  variant: Artwork,
): Promise<MotionRenderer | null> {
  const context = canvas.getContext("2d", { alpha: false });
  if (!context) return null;
  const { background, colors } = signalPalettes[variant];
  let width = 1, height = 1;
  return {
    resize(w, h) {
      width = Math.max(1, Math.round(w));
      height = Math.max(1, Math.round(h));
      if (canvas.width !== width) canvas.width = width;
      if (canvas.height !== height) canvas.height = height;
    },
    draw(seconds) {
      const scale = Math.max(width / 1360, height / 684);
      context.setTransform(1, 0, 0, 1, 0, 0);
      context.fillStyle = background;
      context.fillRect(0, 0, width, height);
      context.setTransform(scale, 0, 0, scale, (width - 1360 * scale) / 2, (height - 684 * scale) / 2);
      context.lineWidth = 15;
      for (let i = 0; i < 34; i++) {
        context.strokeStyle = colors[Math.floor(i / 7) % colors.length];
        context.stroke(new Path2D(signalBand(i, seconds)));
      }
    },
    destroy() { canvas.width = canvas.height = 1; },
  };
}
