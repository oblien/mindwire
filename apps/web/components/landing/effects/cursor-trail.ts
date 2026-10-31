/** A woven wire follows the pointer, using the landing illustrations' palette. */
const colors = ["#6676dc", "#b5d1bd", "#f0d8ad", "#ef795b"];
type Point = { x: number; y: number; time: number };

export function createCursorTrail(canvas: HTMLCanvasElement, host: HTMLElement, onMove: () => void) {
  const context = canvas.getContext("2d");
  if (!context) return null;
  let points: Point[] = [];
  let frame = 0, scale = 1, disposed = false;
  const lifetime = 1100;
  const clear = () => {
    cancelAnimationFrame(frame);
    frame = 0;
    points = [];
    context.clearRect(0, 0, canvas.width / scale, canvas.height / scale);
  };
  const resize = () => {
    scale = Math.min(devicePixelRatio || 1, 1.5);
    canvas.width = Math.round(host.clientWidth * scale);
    canvas.height = Math.round(host.clientHeight * scale);
    context.setTransform(scale, 0, 0, scale, 0, 0);
    clear();
  };
  const draw = (now: number) => {
    frame = 0;
    if (disposed) return;
    points = points.filter(point => now - point.time < lifetime);
    context.clearRect(0, 0, canvas.width / scale, canvas.height / scale);
    if (points.length < 2) return;
    const opacity = Math.max(0, 1 - (now - points.at(-1)!.time) / lifetime);
    colors.forEach((color, channel) => {
      const strand = points.map((point, i) => {
        const next = points[Math.min(i + 1, points.length - 1)];
        const previous = points[Math.max(0, i - 1)];
        const dx = next.x - previous.x, dy = next.y - previous.y;
        const length = Math.hypot(dx, dy) || 1;
        const offset = (channel - 1.5) * 7;
        return { x: point.x - dy / length * offset, y: point.y + dx / length * offset };
      });
      context.beginPath();
      context.moveTo(strand[0].x, strand[0].y);
      for (let i = 1; i < strand.length - 1; i++) {
        context.quadraticCurveTo(strand[i].x, strand[i].y, (strand[i].x + strand[i + 1].x) / 2, (strand[i].y + strand[i + 1].y) / 2);
      }
      const last = strand.at(-1)!;
      context.lineTo(last.x, last.y);
      context.strokeStyle = color;
      context.globalAlpha = opacity;
      context.lineWidth = 4.5;
      context.lineJoin = context.lineCap = "round";
      context.stroke();
    });
    context.globalAlpha = 1;
    frame = requestAnimationFrame(draw);
  };
  const move = (event: PointerEvent) => {
    if (event.pointerType === "touch" || disposed) return;
    const bounds = host.getBoundingClientRect();
    const point = { x: event.clientX - bounds.left, y: event.clientY - bounds.top, time: performance.now() };
    const last = points.at(-1);
    if (last && Math.hypot(last.x - point.x, last.y - point.y) < 4) return;
    points.push(point);
    if (points.length > 60) points.shift();
    onMove();
    if (!frame) frame = requestAnimationFrame(draw);
  };
  const visibility = () => { if (document.hidden) clear(); };
  const observer = new ResizeObserver(resize);
  observer.observe(host);
  host.addEventListener("pointermove", move);
  document.addEventListener("visibilitychange", visibility);
  resize();
  return {
    destroy() {
      disposed = true;
      clear();
      observer.disconnect();
      host.removeEventListener("pointermove", move);
      document.removeEventListener("visibilitychange", visibility);
    },
  };
}
