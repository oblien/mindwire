"use client";

import { useId, useState, type CSSProperties } from "react";
import { ArrowRight, ArrowUpRight, Check, FileText, LayoutGrid, Palette, Plus, Settings2, SlidersHorizontal, Sparkles, WandSparkles, Wrench } from "lucide-react";
import { navigateTabs } from "./tabs";
import styles from "./atelier-home.module.css";

const tabs = [{name:"Your brand",icon:Palette},{name:"Your tools",icon:Wrench},{name:"Your direction",icon:SlidersHorizontal}] as const;
const palettes = [{name:"Vermilion",color:"#ff613e",ink:"#151515"},{name:"Cobalt",color:"#3458ed",ink:"#fff"},{name:"Citron",color:"#e4ec5a",ink:"#151515"}];
const options = ["Read project files", "Create pages", "Publish changes"];

export default function AtelierPlayground() {
  const id = useId();
  const [tab,setTab] = useState(0);
  const [brand,setBrand] = useState("forma");
  const [palette,setPalette] = useState(0);
  const [tools,setTools] = useState(["Read project files","Create pages"]);
  const [instructions,setInstructions] = useState("Use our brand guide. Keep things thoughtful and useful. Ask before publishing.");
  const [created,setCreated] = useState(false);
  const name = brand.trim() || "Your product";
  return (
    <section id="features" className={styles.customization} aria-labelledby="features-title">
      <div className={styles.customHeading}><div><span className={styles.eyebrow}>THEIR BRAINS. YOUR SIGNATURE.</span><h2 id="features-title">Make it look like you.<br /><em>Make it work like you.</em></h2></div><p>People should remember your product. Shape the brand, the tools, and the way your agent works. Try it here.</p></div>
      <div className={styles.playground} style={{"--brand":palettes[palette].color,"--brand-ink":palettes[palette].ink} as CSSProperties}>
        <div className={styles.workbench}>
          <div className={styles.workbenchTabs} role="tablist" aria-label="Customize the example product" onKeyDown={navigateTabs}>
            {tabs.map(({name,icon:Icon},i)=><button key={name} type="button" role="tab" id={`${id}-tab-${i}`} aria-controls={`${id}-controls`} aria-selected={i===tab} tabIndex={i===tab?0:-1} onClick={()=>setTab(i)}><Icon size={19} strokeWidth={1.5} /><span>{name}</span><ArrowUpRight size={17} /></button>)}
          </div>
          <div className={styles.workbenchControls} role="tabpanel" id={`${id}-controls`} aria-labelledby={`${id}-tab-${tab}`}>
            {tab===0 && <><label className={styles.brandName}>PRODUCT NAME<input value={brand} onChange={e=>setBrand(e.target.value)} maxLength={24} /></label><fieldset className={styles.brandPalette}><legend>YOUR COLOR</legend>{palettes.map((item,i)=><button key={item.name} type="button" aria-label={`${item.name} palette`} aria-pressed={palette===i} style={{background:item.color,color:item.ink}} onClick={()=>setPalette(i)}>{palette===i&&<Check size={18}/>}</button>)}</fieldset><p>Your name on the experience.<br />Your identity in every detail.</p></>}
            {tab===1 && <><span className={styles.controlLabel}>WHAT CAN YOUR AGENT DO?</span><div className={styles.toolOptions}>{options.map(option=><label key={option}><input type="checkbox" checked={tools.includes(option)} onChange={()=>setTools(current=>current.includes(option)?current.filter(tool=>tool!==option):[...current,option])}/><span>{option}</span></label>)}</div><p>Choose the actions your product makes available. You control the boundaries.</p></>}
            {tab===2 && <><label className={styles.directionLabel}>HOW SHOULD IT WORK?<textarea value={instructions} onChange={e=>setInstructions(e.target.value)} rows={5} maxLength={220}/></label><p>A role, a point of view, and the rules that matter to your users.</p></>}
          </div>
          <span className={styles.workbenchFoot}>A SMALL EXAMPLE.<br />A VERY BIG CANVAS.</span>
        </div>
        <div className={styles.productStage}>
          <div className={styles.stageCaption}><span><Sparkles size={14}/> YOUR BRAND, IN ACTION</span><span>Interactive preview</span></div>
          <div className={styles.demoApp}>
            <aside className={styles.demoSidebar} aria-hidden="true"><strong>{name.slice(0,1).toLowerCase()}.</strong><LayoutGrid size={19}/><FileText size={19}/><Plus size={19}/><Settings2 size={19}/></aside>
            <div className={styles.demoMain}>
              <div className={styles.demoHeader}><strong>{name}</strong><span>Creative workspace</span><span className={styles.demoAvatar}>J</span></div>
              <div className={styles.demoConversation}><span className={styles.demoUser}>J</span><p>Make a launch page from our retreat brief.</p></div>
              <div className={styles.demoResult}>
                <div className={styles.demoResponse}><span><WandSparkles size={15}/></span><p>{created?"Your page is ready. Every detail, made yours.":"Here’s a direction that feels like you."}</p></div>
                <div className={styles.demoWebsite}><div className={styles.demoWebsiteBar}><span>Fieldwork</span><span>The idea ↗</span></div><div className={styles.demoWebsiteHero}><div><span>A LITTLE SPACE. A NEW PERSPECTIVE.</span><h3>Make room<br />for what’s<br /><em>next.</em></h3><span className={styles.demoWebsiteCta}>Find your space <ArrowUpRight size={13}/></span></div><img src="/images/launch-demo/fieldwork-retreat.webp" alt="A quiet retreat surrounded by nature" width={600} height={700} loading="lazy" /></div></div>
                <div className={styles.demoResultFoot}><span><Check size={13}/> {tools.length} tools available</span><span>{tools.includes("Publish changes")?"Publishing enabled":"Review before publishing"}</span></div>
              </div>
              <div className={styles.demoComposer}><span>{created?"A good idea, made real.":"A little more you. A lot more possible."}</span><button type="button" aria-label={created?"Reset product example":"Create example page"} onClick={()=>setCreated(value=>!value)}>{created?<Check size={16}/>:<ArrowRight size={16}/>}</button></div>
            </div>
          </div>
          <div className={styles.stageNote}><FileText size={16}/><p>{tab===2?instructions||"Give your agent a direction.":"Your agent. Your interface. Your users never have to leave."}</p><span>↗</span></div>
        </div>
      </div>
    </section>
  );
}
