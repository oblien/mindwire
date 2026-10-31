"use client";
import { useEffect, useRef } from "react";

/** Original continuous vector ribbons. Canvas is a resolution-independent drawing surface. */
export default function KineticField({ className = "", compact = false }: { className?: string; compact?: boolean }) {
  const ref=useRef<HTMLCanvasElement>(null);
  useEffect(()=>{
    const canvas=ref.current;
    if(!canvas)return;
    const ctx=canvas.getContext("2d");
    if(!ctx)return;
    const motion=matchMedia("(prefers-reduced-motion: reduce)");
    let frame=0,w=1,h=1,time=0,last=0,visible=false,pointer=0,target=0;
    const draw=(now:number)=>{
      frame=0;
      if(!visible||document.hidden)return;
      const delta=last?Math.min((now-last)/1000,.05):0;
      last=now;time+=motion.matches?0:delta;
      pointer+=(target-pointer)*.045;
      ctx.clearRect(0,0,w,h);
      const phase=time*.22;
      ctx.save();ctx.scale(w/1440,h/760);
      const shift=(motion.matches?0:Math.sin(phase)*38)+pointer*32;
      // Each closed band shares two flowing Bezier edges; their overlaps create the material.
      for(let i=0;i<18;i++){
        const k=i-9,offset=k*13;
        const path=new Path2D(`M-120 ${300+offset}C150 ${300+offset+shift} 300 ${780+offset-shift} 650 ${520+offset}C1000 ${260+offset} 1010 ${-10+offset+shift} 1560 ${300+offset}L1560 ${325+offset}C1010 ${15+offset+shift} 1000 ${285+offset} 650 ${545+offset}C300 ${805+offset-shift} 150 ${325+offset+shift} -120 ${325+offset}Z`);
        const gradient=ctx.createLinearGradient(-100,550,1450,200);
        const mix=i/17;
        gradient.addColorStop(0,`hsl(${12+mix*13} 96% ${47+mix*17}%)`);
        gradient.addColorStop(.34,`hsl(${325-mix*25} 87% ${55+mix*24}%)`);
        gradient.addColorStop(.62,`hsl(${250-mix*22} 94% ${45+mix*28}%)`);
        gradient.addColorStop(1,`hsl(${25+mix*20} 96% ${48+mix*25}%)`);
        ctx.fillStyle=gradient;ctx.fill(path);
        ctx.strokeStyle=`rgba(255,255,255,${.07+(i%3)*.025})`;ctx.lineWidth=.8;ctx.stroke(path);
      }
      ctx.restore();
      if(!motion.matches)frame=requestAnimationFrame(draw);
    };
    const start=()=>{ if(!frame&&visible&&!document.hidden){last=0;frame=requestAnimationFrame(draw);} };
    const stop=()=>{cancelAnimationFrame(frame);frame=0;last=0;};
    const resize=()=>{const rect=canvas.getBoundingClientRect();w=rect.width;h=rect.height;const dpr=Math.min(devicePixelRatio||1,1.5);canvas.width=Math.round(w*dpr);canvas.height=Math.round(h*dpr);ctx.setTransform(dpr,0,0,dpr,0,0);stop();start();};
    const move=(event:PointerEvent)=>{if(motion.matches||event.pointerType==="touch")return;const r=canvas.getBoundingClientRect();target=(event.clientX-r.left)/r.width-.5;};
    const parent=canvas.parentElement;
    const observer=new ResizeObserver(resize);observer.observe(canvas);
    const intersection=new IntersectionObserver(([entry])=>{visible=entry.isIntersecting;if(visible)start();else stop();},{rootMargin:"80px"});intersection.observe(canvas);
    const update=()=>{stop();start();};const visibility=()=>document.hidden?stop():start();
    parent?.addEventListener("pointermove",move,{passive:true});motion.addEventListener("change",update);document.addEventListener("visibilitychange",visibility);resize();
    return()=>{stop();observer.disconnect();intersection.disconnect();parent?.removeEventListener("pointermove",move);motion.removeEventListener("change",update);document.removeEventListener("visibilitychange",visibility);};
  },[compact]);
  return <canvas ref={ref} className={className} aria-hidden="true" data-kinetic-field />;
}
