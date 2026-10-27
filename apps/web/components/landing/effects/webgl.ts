/** Small WebGL surface shared by the cursor and artwork renderers. */
export function createSurface(
  canvas: HTMLCanvasElement,
  fragment: string,
  alpha = false,
) {
  const gl = canvas.getContext("webgl", {
    alpha,
    premultipliedAlpha: true,
    antialias: false,
    depth: false,
    powerPreference: "low-power",
  });
  if (!gl) return null;
  const shaders: WebGLShader[] = [];
  const compile = (type: number, source: string) => {
    const shader = gl.createShader(type);
    if (!shader) return null;
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      gl.deleteShader(shader);
      return null;
    }
    shaders.push(shader);
    return shader;
  };
  const vertex = compile(
    gl.VERTEX_SHADER,
    "attribute vec2 a;void main(){gl_Position=vec4(a,0.,1.);}",
  );
  const pixel = compile(gl.FRAGMENT_SHADER, fragment);
  const program = gl.createProgram();
  if (!program || !vertex || !pixel) {
    shaders.forEach((shader) => gl.deleteShader(shader));
    if (program) gl.deleteProgram(program);
    return null;
  }
  gl.attachShader(program, vertex);
  gl.attachShader(program, pixel);
  gl.bindAttribLocation(program, 0, "a");
  gl.linkProgram(program);
  shaders.forEach((shader) => gl.deleteShader(shader));
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    gl.deleteProgram(program);
    return null;
  }
  gl.useProgram(program);
  const buffer = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
  gl.bufferData(
    gl.ARRAY_BUFFER,
    new Float32Array([-1, -1, 3, -1, -1, 3]),
    gl.STATIC_DRAW,
  );
  gl.enableVertexAttribArray(0);
  gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
  gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
  const textures: WebGLTexture[] = [];
  return {
    gl,
    uniform: (name: string) => gl.getUniformLocation(program, name),
    texture(unit: number) {
      const texture = gl.createTexture();
      if (!texture) throw new Error("WebGL texture unavailable");
      gl.activeTexture(gl.TEXTURE0 + unit);
      gl.bindTexture(gl.TEXTURE_2D, texture);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      textures.push(texture);
      return texture;
    },
    resize(width: number, height: number): [number, number] {
      width = Math.max(1, Math.round(width));
      height = Math.max(1, Math.round(height));
      if (canvas.width !== width || canvas.height !== height) {
        canvas.width = width;
        canvas.height = height;
      }
      gl.viewport(0, 0, width, height);
      return [width, height];
    },
    draw: () => gl.drawArrays(gl.TRIANGLES, 0, 3),
    destroy() {
      textures.forEach((texture) => gl.deleteTexture(texture));
      gl.deleteBuffer(buffer);
      gl.deleteProgram(program);
    },
  };
}

export type Surface = NonNullable<ReturnType<typeof createSurface>>;
export type MotionRenderer = {
  resize: (width: number, height: number) => void;
  draw: (seconds: number) => void;
  destroy: () => void;
};
export const smooth = (value: number) => value * value * (3 - 2 * value);
export const rgb = (hex: string) =>
  [1, 3, 5].map((start) => parseInt(hex.slice(start, start + 2), 16));
