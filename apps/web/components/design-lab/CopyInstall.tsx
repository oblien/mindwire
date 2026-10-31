"use client";
import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";
import styles from "./studies.module.css";
export default function CopyInstall() {
  const [status,setStatus]=useState("Copy command");
  const timeout=useRef<ReturnType<typeof setTimeout>|null>(null);
  useEffect(()=>()=>{if(timeout.current)clearTimeout(timeout.current);},[]);
  async function copy(){
    try {await navigator.clipboard.writeText("npm install mindwire");setStatus("Copied");}
    catch {setStatus("Select the command to copy");}
    if(timeout.current)clearTimeout(timeout.current);
    timeout.current=setTimeout(()=>setStatus("Copy command"),2400);
  }
  return <div className={styles.install}><code><span>$</span> npm install mindwire</code><button onClick={copy} aria-label={status} title={status}>{status==="Copied"?<Check size={17}/>:<Copy size={17}/>}</button><span className={styles.srOnly} role="status">{status!=="Copy command"?status:""}</span></div>;
}
