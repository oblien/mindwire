import scenes from "./art-scenes.json";
import { fieldFragment, seedFragment } from "./art-shaders";
import { createSurface, rgb, smooth, type MotionRenderer } from "./webgl";

export type Artwork = keyof typeof scenes | "closing";
type ColorStop = [number, string];

function colorRamp(stops: ColorStop[]) {
  const sorted = [...stops].sort((a, b) => a[0] - b[0]);
  const pixels = new Uint8Array(512 * 4);
  for (let index = 0; index < 512; index++) {
    const position = index / 511;
    let left = sorted[0],
      right = sorted.at(-1)!;
    for (let step = 0; step < sorted.length - 1; step++) {
      if (position >= sorted[step][0] && position <= sorted[step + 1][0]) {
        left = sorted[step];
        right = sorted[step + 1];
        break;
      }
    }
    if (position < sorted[0][0]) left = right = sorted[0];
    if (position > sorted.at(-1)![0]) left = right = sorted.at(-1)!;
    const fraction = smooth(
      left[0] === right[0] ? 0 : (position - left[0]) / (right[0] - left[0]),
    );
    const a = rgb(left[1]),
      b = rgb(right[1]);
    pixels.set(
      [
        ...a.map((channel, axis) =>
          Math.round(channel + (b[axis] - channel) * fraction),
        ),
        255,
      ],
      index * 4,
    );
  }
  return pixels;
}

function createSeedRenderer(
  canvas: HTMLCanvasElement,
  variant: keyof typeof scenes,
): MotionRenderer | null {
  const scene = scenes[variant];
  const surface = createSurface(canvas, seedFragment);
  if (!surface) return null;
  const { gl, uniform } = surface;
  surface.texture(0);
  gl.texImage2D(
    gl.TEXTURE_2D,
    0,
    gl.RGBA,
    512,
    1,
    0,
    gl.RGBA,
    gl.UNSIGNED_BYTE,
    colorRamp(scene.colors as ColorStop[]),
  );
  gl.uniform1i(uniform("uRamp"), 0);
  const blobs = new Float32Array(64),
    shapes = new Float32Array(64);
  scene.blobs.slice(0, 16).forEach((blob, index) => {
    blobs.set(blob.slice(0, 4), index * 4);
    shapes.set(blob.slice(4, 8), index * 4);
  });
  gl.uniform4fv(uniform("uB"), blobs);
  gl.uniform4fv(uniform("uS"), shapes);
  gl.uniform1i(uniform("uN"), scene.blobs.length);
  gl.uniform1f(uniform("uSeed"), scene.seed);
  const parameters = {
    uWarp: "warp",
    uWarpScale: "warpScale",
    uNoise: "noise",
    uNoiseScale: "noiseScale",
    uFlow: "flow",
    uBase: "base",
    uContrast: "contrast",
    uOffset: "offset",
    uGrain: "grain",
    uGrainSize: "grainSize",
    uVig: "vignette",
    uPaper: "paper",
    uOrbit: "orbit",
    uRing: "ring",
    uRingR: "ringR",
    uRingW: "ringW",
    uDust: "dust",
  } as const;
  for (const [name, key] of Object.entries(parameters))
    gl.uniform1f(uniform(name), scene.parameters[key]);
  gl.uniform1i(uniform("uAnim"), scene.animation === "split" ? 4 : 0);
  gl.uniform1i(uniform("uDepth"), scene.depth);
  gl.uniform1f(uniform("uSplitSep"), scene.motion.splitSep);
  // These scenes use continuous color. Disable the renderer's optional pixel grid.
  gl.uniform1i(uniform("uPx"), 0);
  const phaseUniform = uniform("uPh"),
    timeUniform = uniform("uT"),
    split = [uniform("uSp1"), uniform("uSp2"), uniform("uSp3")];
  const step = (start: number, end: number, value: number) =>
    smooth(Math.min(1, Math.max(0, (value - start) / (end - start))));
  return {
    resize(width, height) {
      const [w, h] = surface.resize(width, height);
      const coverWidth = Math.max(w, h * scene.aspect),
        coverHeight = Math.max(h, w / scene.aspect);
      gl.uniform2f(uniform("uRes"), coverWidth, coverHeight);
      gl.uniform2f(
        uniform("uOff"),
        (coverWidth - w) / 2,
        (coverHeight - h) / 2,
      );
    },
    draw(seconds) {
      const phase =
        (scene.phase + (Math.PI * 2 * seconds) / scene.parameters.loop) %
        (Math.PI * 2);
      gl.uniform1f(phaseUniform, phase);
      gl.uniform1f(timeUniform, scene.time + seconds);
      const progress = phase / (Math.PI * 2),
        fade = scene.animation === "split" ? 1 - step(0.78, 0.98, progress) : 0;
      gl.uniform1f(split[0], step(0.18, 0.36, progress) * fade);
      gl.uniform1f(split[1], step(0.36, 0.52, progress) * fade);
      gl.uniform1f(split[2], step(0.52, 0.66, progress) * fade);
      surface.draw();
    },
    destroy: surface.destroy,
  };
}

async function createClosingRenderer(
  canvas: HTMLCanvasElement,
): Promise<MotionRenderer | null> {
  const field = new Image();
  field.src = "/images/framework/closing-field.webp";
  await field.decode();
  const surface = createSurface(canvas, fieldFragment);
  if (!surface) return null;
  const { gl, uniform } = surface;
  const stops: ColorStop[] = [
    [0, "#141316"],
    [0.3, "#1c1b1f"],
    [0.42, "#24215e"],
    [0.47, "#5354ee"],
    [0.51, "#7c7aff"],
    [0.55, "#e38ac2"],
    [0.59, "#ec5f5e"],
    [0.66, "#e8513a"],
    [0.84, "#ee6b44"],
    [1, "#f39a55"],
  ];
  surface.texture(0);
  gl.texImage2D(
    gl.TEXTURE_2D,
    0,
    gl.RGBA,
    512,
    1,
    0,
    gl.RGBA,
    gl.UNSIGNED_BYTE,
    colorRamp(stops),
  );
  surface.texture(1);
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, field);
  gl.uniform1i(uniform("uRamp"), 0);
  gl.uniform1i(uniform("uField"), 1);
  const aspect = field.naturalWidth / field.naturalHeight;
  gl.uniform1f(uniform("uAspect"), aspect);
  const resolution = uniform("uRes"),
    cover = uniform("uCover"),
    phase = uniform("uPhase"),
    amplitude = uniform("uAmp");
  return {
    resize(width, height) {
      const [w, h] = surface.resize(width, height);
      const ratio = w / h;
      gl.uniform2f(resolution, w, h);
      gl.uniform2f(
        cover,
        ratio > aspect ? 1 : ratio / aspect,
        ratio > aspect ? aspect / ratio : 1,
      );
    },
    draw(seconds) {
      gl.uniform1f(phase, ((seconds % 20) / 20) * Math.PI * 2);
      gl.uniform1f(amplitude, smooth(Math.min(1, seconds / 2.5)));
      surface.draw();
    },
    destroy: surface.destroy,
  };
}

export async function createArtwork(
  canvas: HTMLCanvasElement,
  variant: Artwork,
) {
  return variant === "closing"
    ? createClosingRenderer(canvas)
    : createSeedRenderer(canvas, variant);
}
