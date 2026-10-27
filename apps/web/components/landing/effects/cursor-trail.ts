import { createSurface, rgb, smooth } from "./webgl";

// The reference draws soft unions of rounded rectangles, then blends their
// colors in sequence. A blurred line or a row of CSS circles cannot match it.
const fragment = `precision highp float;
uniform vec2 uRes;
uniform float uScale;
uniform int uN;
uniform vec4 uR[64],uC[64];
float roundedBox(vec2 p,vec2 b,float radius){
  radius=min(radius,min(b.x,b.y));
  vec2 q=abs(p)-b+radius;
  return length(max(q,0.))+min(max(q.x,q.y),0.)-radius;
}
void main(){
  vec2 p=vec2(gl_FragCoord.x,uRes.y-gl_FragCoord.y)/uScale;
  float field=0.;
  vec3 color=vec3(0.);
  float remaining=1.;
  for(int i=0;i<64;i++){
    if(i>=uN)break;
    float distance=roundedBox(p-uR[i].xy,uR[i].zw,18.);
    float edge=max(distance+1.1774*12.,0.);
    field+=exp(-edge*edge/(2.*12.*12.));
    float weight=1./(1.+exp(clamp(distance/10.,-30.,30.)));
    color=mix(color,uC[i].rgb,weight);
    remaining*=1.-weight;
  }
  color+=remaining*vec3(1.);
  float alpha=smoothstep(.42,.58,field);
  gl_FragColor=vec4(color*alpha,alpha);
}`;

const colors = ["#d280ff", "#ff8957", "#fff45c", "#5280ff", "#ff554d"].map(
  (hex) => rgb(hex).map((channel) => channel / 255),
);
type Block = {
  x: number;
  y: number;
  width: number;
  height: number;
  color: number[];
  born: number;
};
type Pointer = {
  x: number;
  y: number;
  time: number;
  speed: number;
  distance: number;
  dx: number;
  dy: number;
};

export function createCursorTrail(
  canvas: HTMLCanvasElement,
  host: HTMLElement,
  onMove: () => void,
) {
  const surface = createSurface(canvas, fragment, true);
  if (!surface) return null;
  const { gl, uniform } = surface;
  const resolution = uniform("uRes"),
    scaleUniform = uniform("uScale"),
    count = uniform("uN"),
    boxes = uniform("uR"),
    palette = uniform("uC");
  const boxData = new Float32Array(256),
    colorData = new Float32Array(256);
  const reduced = matchMedia("(prefers-reduced-motion: reduce)");
  let blocks: Block[] = [],
    previous: Pointer | null = null,
    colorIndex = 0,
    frame = 0,
    scale = 1,
    disposed = false;

  const draw = (now: number) => {
    frame = 0;
    if (disposed) return;
    blocks = blocks.filter((block) => now - block.born < 750);
    blocks.forEach((block, index) => {
      const age = now - block.born;
      const growth = Math.min(1, age / 175);
      const size = (1 - (1 - growth) ** 2) * (1 - (age / 750) ** 3);
      boxData.set(
        [block.x, block.y, (block.width / 2) * size, (block.height / 2) * size],
        index * 4,
      );
      colorData.set([...block.color, 1], index * 4);
    });
    gl.clearColor(0, 0, 0, 0);
    gl.clear(gl.COLOR_BUFFER_BIT);
    if (blocks.length) {
      gl.uniform2f(resolution, canvas.width, canvas.height);
      gl.uniform1f(scaleUniform, scale);
      gl.uniform1i(count, blocks.length);
      gl.uniform4fv(boxes, boxData);
      gl.uniform4fv(palette, colorData);
      surface.draw();
      frame = requestAnimationFrame(draw);
    }
  };
  const kick = () => {
    if (!frame && !disposed) frame = requestAnimationFrame(draw);
  };
  const spawn = (x: number, y: number, size: number, pointer: Pointer) => {
    const length = Math.hypot(pointer.dx, pointer.dy) || 1;
    blocks.push({
      x,
      y,
      width: size * (1 + 0.69 * Math.abs(pointer.dx / length)),
      height: size * (1 + 0.69 * Math.abs(pointer.dy / length)),
      color: colors[colorIndex++ % colors.length],
      born: performance.now(),
    });
    if (blocks.length > 64) blocks.shift();
    kick();
  };
  const move = (event: PointerEvent) => {
    if (event.pointerType === "touch" || reduced.matches || disposed) return;
    onMove();
    const rect = host.getBoundingClientRect();
    const x = event.clientX - rect.left,
      y = event.clientY - rect.top,
      now = performance.now();
    if (!previous) {
      previous = { x, y, time: now, speed: 0, distance: 0, dx: 1, dy: 0 };
      spawn(x, y, 30, previous);
      return;
    }
    const dx = x - previous.x,
      dy = y - previous.y,
      distance = Math.hypot(dx, dy),
      elapsed = Math.max(1, now - previous.time);
    previous.speed += (distance / elapsed - previous.speed) * 0.3;
    if (distance > 0.5) {
      previous.dx += (dx / distance - previous.dx) * 0.5;
      previous.dy += (dy / distance - previous.dy) * 0.5;
    }
    const size = 30 + 41 * smooth(Math.min(1, previous.speed / 1.75));
    const spacing = Math.max(4, size * 1.31),
      accumulated = previous.distance;
    previous.distance += distance;
    if (previous.distance >= spacing && distance > 0) {
      const steps = Math.floor(previous.distance / spacing);
      for (let step = Math.max(1, steps - 11); step <= steps; step++) {
        const fraction = Math.max(
          0,
          Math.min(1, (step * spacing - accumulated) / distance),
        );
        spawn(
          previous.x + dx * fraction,
          previous.y + dy * fraction,
          size,
          previous,
        );
      }
      previous.distance -= steps * spacing;
    }
    Object.assign(previous, { x, y, time: now });
  };
  const leave = () => {
    previous = null;
  };
  const clear = () => {
    blocks = [];
    previous = null;
    kick();
  };
  const resize = () => {
    const rect = host.getBoundingClientRect();
    scale = 0.75 * Math.min(devicePixelRatio || 1, 2);
    surface.resize(rect.width * scale, rect.height * scale);
    kick();
  };
  const visibility = () => {
    if (document.hidden) {
      cancelAnimationFrame(frame);
      frame = 0;
      clear();
    }
  };
  const observer = new ResizeObserver(resize);
  observer.observe(host);
  resize();
  host.addEventListener("pointermove", move);
  host.addEventListener("pointerleave", leave);
  reduced.addEventListener("change", clear);
  document.addEventListener("visibilitychange", visibility);
  return {
    destroy() {
      disposed = true;
      cancelAnimationFrame(frame);
      observer.disconnect();
      host.removeEventListener("pointermove", move);
      host.removeEventListener("pointerleave", leave);
      reduced.removeEventListener("change", clear);
      document.removeEventListener("visibilitychange", visibility);
      surface.destroy();
    },
  };
}
