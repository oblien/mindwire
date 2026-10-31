import styles from "./studies.module.css";

export default function SignalScope() {
  return <div className={styles.scope} aria-label="Illustration of your product connecting to the Mindwire agent runtime" role="img">
    <div className={styles.scopeTop}><span>MW—01 / SIGNAL PROCESSOR</span><span>● ONLINE</span></div>
    <svg viewBox="0 0 640 440" fill="none" aria-hidden="true">
      <defs><pattern id="scope-grid" width="24" height="24" patternUnits="userSpaceOnUse"><path d="M24 0H0V24" stroke="#c9f391" strokeOpacity=".09" strokeWidth=".5"/></pattern><linearGradient id="scope-signal" x1="100" y1="170" x2="540" y2="290" gradientUnits="userSpaceOnUse"><stop stopColor="#dafc9d"/><stop offset=".5" stopColor="#e9ffc8"/><stop offset="1" stopColor="#6bca9a"/></linearGradient></defs>
      <rect width="640" height="440" fill="url(#scope-grid)"/>
      <path d="M0 220H640M320 0V440" stroke="#ddffa9" strokeOpacity=".16" strokeDasharray="3 7"/>
      <circle cx="320" cy="220" r="171" stroke="#c9f391" strokeOpacity=".16"/><circle cx="320" cy="220" r="126" stroke="#c9f391" strokeOpacity=".11" strokeDasharray="2 5"/>
      <g className={styles.scopeWave}>{Array.from({length:77},(_,index)=>{
        const x=82+index*6.25;
        const envelope=Math.pow(Math.sin(index/76*Math.PI),.65);
        const height=12+(Math.sin(index*.26)**2*142+Math.sin(index*.087)**2*63)*envelope;
        return <rect key={index} x={x} y={220-height/2} width="2.5" height={height} fill="url(#scope-signal)" style={{animationDelay:`${index*-.07}s`}}/>;
      })}</g>
      <g stroke="#ddffa9" strokeOpacity=".55"><path d="M26 42V26H42M598 26H614V42M614 398V414H598M42 414H26V398"/><path d="M315 49H325M320 44V54M315 391H325M320 386V396"/></g>
      <g fill="#9aaf91" fontFamily="monospace" fontSize="9"><text x="45" y="61">INPUT / YOUR WORLD</text><text x="449" y="393">OUTPUT / POSSIBILITY</text><text x="45" y="393">FREQ. 001—077</text><text x="513" y="61">∞ CHANNELS</text></g>
    </svg>
    <div className={styles.scopeReadout}><div><span>CONTEXT</span><b>CONNECTED</b></div><div><span>AGENT</span><b>YOUR CHOICE</b></div><div><span>INTERFACE</span><b>YOURS</b></div></div>
  </div>;
}
