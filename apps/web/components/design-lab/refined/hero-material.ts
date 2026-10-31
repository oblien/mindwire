import { heroSurfaces, type HeroVariant } from "./hero-surfaces";

const vertexSource = `
attribute vec2 a_position;
void main() { gl_Position = vec4(a_position, 0.0, 1.0); }
`;

const fragmentSource = `
precision highp float;
uniform vec2 u_resolution;
uniform vec2 u_size;
uniform vec2 u_pointer;
uniform float u_time;
uniform float u_strength;
uniform float u_anchor;
uniform float u_motion;
uniform sampler2D u_surface;

float softLight(float offset, float width) {
  float d = offset / width;
  return exp(-d * d);
}

void main() {
  vec2 screen = gl_FragCoord.xy / u_resolution;
  vec2 p = vec2(screen.x, 1.0 - screen.y);
  float side = pow(abs(p.x - 0.5) * 2.0, 2.5);
  float reveal = smoothstep(0.72 - side * 0.26, 0.97 - side * 0.26, p.y);
  if (u_motion > 0.5) {
    reveal = smoothstep(0.66 - side * 0.20, 0.90 - side * 0.15, p.y);
  }
  float t = u_time;
  float crop = min(1.0, u_size.x / u_size.y / 2.0);
  vec2 material = vec2(p.x * crop + (1.0 - crop) * u_anchor, p.y);

  // Slow changes in refraction keep the glass surface alive without moving type.
  float fold = sin(p.y * 8.0 + t * 0.28) + 0.45 * sin(p.x * 7.0 - t * 0.19);
  float flute = sin(p.x * 115.0 + sin(p.y * 4.0 - t * 0.2) * 1.4);
  vec2 bend = vec2(fold * 0.009 + flute * 0.0014, sin(p.x * 6.0 - t * 0.24) * 0.007);
  if (u_motion > 0.5) {
    // Rigid optical planes keep their geometry as the lighting changes.
    bend = vec2(0.0);
  }
  bend += u_pointer * vec2(0.005, 0.003);
  float edge = 1.0 - smoothstep(0.94, 1.0, p.y);
  vec2 uv = clamp(material + bend * vec2(crop, 1.0) * reveal * edge, vec2(0.001), vec2(0.999));
  float fringe = (u_motion > 0.5 ? 0.0002 : 0.0005 + 0.0004 * sin(p.y * 5.0 + t * 0.2)) * reveal;
  vec3 glass = vec3(
    texture2D(u_surface, uv + vec2(fringe, 0.0)).r,
    texture2D(u_surface, uv).g,
    texture2D(u_surface, uv - vec2(fringe, 0.0)).b
  );
  if (u_motion > 0.5) {
    // A soft moving reflection catches the existing bevels and contact edges.
    float position = material.x * 0.72 + material.y * 0.3;
    float light = softLight(position - (0.5 + 0.65 * sin(t * 0.22)), 0.11);
    vec2 sampleOffset = vec2(0.0015, -0.001);
    vec3 nearbyA = texture2D(u_surface, clamp(uv + sampleOffset, vec2(0.001), vec2(0.999))).rgb;
    vec3 nearbyB = texture2D(u_surface, clamp(uv - sampleOffset, vec2(0.001), vec2(0.999))).rgb;
    float bevel = clamp(length(nearbyA - nearbyB) * 3.0, 0.0, 1.0);
    glass += vec3(0.9, 0.95, 1.0) * light * (0.018 + bevel * 0.09) * reveal;
  } else {
    float sweep = pow(0.5 + 0.5 * sin(p.x * 4.0 - p.y * 2.5 - t * 0.24), 5.0);
    glass = mix(glass, vec3(1.0, 0.99, 0.97), sweep * 0.085 * reveal);
  }
  vec3 paper = vec3(248.0, 247.0, 243.0) / 255.0;
  gl_FragColor = vec4(mix(paper, glass, reveal * u_strength), 1.0);
}
`;

type Resources = {
  program: WebGLProgram;
  buffer: WebGLBuffer;
  texture: WebGLTexture;
  vertex: WebGLShader;
  fragment: WebGLShader;
  resolution: WebGLUniformLocation | null;
  size: WebGLUniformLocation | null;
  time: WebGLUniformLocation | null;
  pointer: WebGLUniformLocation | null;
  surface: WebGLUniformLocation | null;
  strength: WebGLUniformLocation | null;
  anchor: WebGLUniformLocation | null;
  motion: WebGLUniformLocation | null;
};

export function createHeroMaterial(
  canvas: HTMLCanvasElement,
  host: HTMLElement,
  variant: HeroVariant,
) {
  const surfaceOptions = heroSurfaces[variant];
  const gl = canvas.getContext("webgl", {
    alpha: false,
    antialias: false,
    depth: false,
    stencil: false,
    powerPreference: "low-power",
  });
  if (!gl) return null;
  let resources: Resources | null = null;
  let frame = 0;
  let last = 0;
  let elapsed = 8;
  let width = 1;
  let height = 1;
  let visible = true;
  let paused = false;
  let disposed = false;
  let textureReady = false;
  const surfaceImage = new Image();
  surfaceImage.decoding = "async";
  const pointer = { x: 0, y: 0 };
  const target = { x: 0, y: 0 };
  const reducedMotion = matchMedia("(prefers-reduced-motion: reduce)");

  const release = () => {
    if (!resources) return;
    gl.deleteBuffer(resources.buffer);
    gl.deleteTexture(resources.texture);
    gl.deleteProgram(resources.program);
    gl.deleteShader(resources.vertex);
    gl.deleteShader(resources.fragment);
    resources = null;
  };
  const initialize = () => {
    const vertex = gl.createShader(gl.VERTEX_SHADER);
    const fragment = gl.createShader(gl.FRAGMENT_SHADER);
    const program = gl.createProgram();
    const buffer = gl.createBuffer();
    const texture = gl.createTexture();
    const discard = () => {
      gl.deleteShader(vertex);
      gl.deleteShader(fragment);
      gl.deleteProgram(program);
      gl.deleteBuffer(buffer);
      gl.deleteTexture(texture);
      return false;
    };
    if (!vertex || !fragment || !program || !buffer || !texture)
      return discard();
    gl.shaderSource(vertex, vertexSource);
    gl.shaderSource(fragment, fragmentSource);
    gl.compileShader(vertex);
    gl.compileShader(fragment);
    if (
      !gl.getShaderParameter(vertex, gl.COMPILE_STATUS) ||
      !gl.getShaderParameter(fragment, gl.COMPILE_STATUS)
    )
      return discard();
    gl.attachShader(program, vertex);
    gl.attachShader(program, fragment);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) return discard();
    gl.useProgram(program);
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(
      gl.ARRAY_BUFFER,
      new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]),
      gl.STATIC_DRAW,
    );
    const position = gl.getAttribLocation(program, "a_position");
    gl.enableVertexAttribArray(position);
    gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    resources = {
      program,
      buffer,
      texture,
      vertex,
      fragment,
      resolution: gl.getUniformLocation(program, "u_resolution"),
      size: gl.getUniformLocation(program, "u_size"),
      time: gl.getUniformLocation(program, "u_time"),
      pointer: gl.getUniformLocation(program, "u_pointer"),
      surface: gl.getUniformLocation(program, "u_surface"),
      strength: gl.getUniformLocation(program, "u_strength"),
      anchor: gl.getUniformLocation(program, "u_anchor"),
      motion: gl.getUniformLocation(program, "u_motion"),
    };
    return true;
  };
  if (!initialize()) return null;

  const draw = () => {
    if (!resources || !textureReady || disposed || gl.isContextLost()) return;
    gl.viewport(0, 0, canvas.width, canvas.height);
    gl.uniform2f(resources.resolution, canvas.width, canvas.height);
    gl.uniform2f(resources.size, width, height);
    gl.uniform1f(resources.time, elapsed);
    gl.uniform1f(resources.strength, surfaceOptions.strength);
    gl.uniform1f(resources.anchor, surfaceOptions.anchor);
    gl.uniform1f(resources.motion, surfaceOptions.motion);
    gl.uniform2f(resources.pointer, pointer.x, pointer.y);
    gl.uniform1i(resources.surface, 0);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    host.dataset.renderer = "webgl";
  };
  const canAnimate = () =>
    !paused &&
    !reducedMotion.matches &&
    visible &&
    !document.hidden &&
    !!resources &&
    textureReady;
  const tick = (now: number) => {
    if (!canAnimate() || disposed) {
      frame = 0;
      return;
    }
    if (!last || now - last >= 1000 / 30) {
      elapsed += last ? Math.min((now - last) / 1000, 0.1) : 0;
      last = now;
      pointer.x += (target.x - pointer.x) * 0.065;
      pointer.y += (target.y - pointer.y) * 0.065;
      draw();
    }
    frame = requestAnimationFrame(tick);
  };
  const sync = () => {
    cancelAnimationFrame(frame);
    frame = 0;
    last = 0;
    if (canAnimate()) frame = requestAnimationFrame(tick);
  };
  const uploadSurface = () => {
    if (
      !resources ||
      disposed ||
      !surfaceImage.naturalWidth ||
      gl.isContextLost()
    )
      return;
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, resources.texture);
    gl.texImage2D(
      gl.TEXTURE_2D,
      0,
      gl.RGBA,
      gl.RGBA,
      gl.UNSIGNED_BYTE,
      surfaceImage,
    );
    textureReady = true;
    draw();
    sync();
  };
  const resize = () => {
    width = host.clientWidth;
    height = host.clientHeight;
    if (!width || !height) return;
    // Bound the total fill rate on large and high-density displays.
    const scale = Math.min(
      devicePixelRatio || 1,
      1.5,
      Math.sqrt(1_300_000 / (width * height)),
    );
    canvas.width = Math.round(width * scale);
    canvas.height = Math.round(height * scale);
    draw();
  };
  const move = (event: PointerEvent) => {
    if (event.pointerType === "touch" || !canAnimate()) return;
    const bounds = host.getBoundingClientRect();
    target.x = ((event.clientX - bounds.left) / bounds.width - 0.5) * 2;
    target.y = ((event.clientY - bounds.top) / bounds.height - 0.5) * 2;
  };
  const resetPointer = () => {
    target.x = 0;
    target.y = 0;
  };
  const lost = (event: Event) => {
    event.preventDefault();
    cancelAnimationFrame(frame);
    frame = 0;
    resources = null;
    textureReady = false;
    delete host.dataset.renderer;
  };
  const restored = () => {
    if (initialize()) {
      resize();
      uploadSurface();
    }
  };
  const resizeObserver = new ResizeObserver(resize);
  const intersectionObserver = new IntersectionObserver(([entry]) => {
    visible = entry.isIntersecting;
    sync();
  });
  resizeObserver.observe(host);
  intersectionObserver.observe(host);
  const surface = host.parentElement!;
  surface.addEventListener("pointermove", move, { passive: true });
  surface.addEventListener("pointerleave", resetPointer);
  document.addEventListener("visibilitychange", sync);
  reducedMotion.addEventListener("change", sync);
  canvas.addEventListener("webglcontextlost", lost);
  canvas.addEventListener("webglcontextrestored", restored);
  resize();
  surfaceImage.onload = uploadSurface;
  surfaceImage.src = surfaceOptions.image;

  return {
    setPaused(value: boolean) {
      paused = value;
      sync();
    },
    dispose() {
      disposed = true;
      surfaceImage.onload = null;
      cancelAnimationFrame(frame);
      resizeObserver.disconnect();
      intersectionObserver.disconnect();
      surface.removeEventListener("pointermove", move);
      surface.removeEventListener("pointerleave", resetPointer);
      document.removeEventListener("visibilitychange", sync);
      reducedMotion.removeEventListener("change", sync);
      canvas.removeEventListener("webglcontextlost", lost);
      canvas.removeEventListener("webglcontextrestored", restored);
      release();
      delete host.dataset.renderer;
    },
  };
}
