// Procedural fields and color mapping recovered from the supplied reference.
// Kept separate from React and lifecycle code so the artwork can be customized.

export const seedFragment = `precision highp float;
uniform vec2 uRes;uniform float uPh,uT,uSeed;
uniform vec2 uOff;
#define HF_FC (gl_FragCoord.xy+uOff)
uniform vec4 uB[16];uniform vec4 uS[16];uniform int uN;
uniform int uAnim,uDepth;uniform vec2 uWDir;uniform float uWPos,uWW,uWAmt,uSp1,uSp2,uSp3,uSplitSep,uDust;
uniform float uWarp,uWarpScale,uNoise,uNoiseScale,uFlow,uBase,uContrast,uOffset,uGrain,uGrainSize,uVig,uPaper,uOrbit,uRing,uRingR,uRingW;
uniform sampler2D uRamp;
uniform int uPxGap;uniform vec3 uPaperCol;
#define HF_TEX texture2D

uniform int uPx,uHtTone,uHtInk;uniform float uPxCell,uPxDot,uPxHeat,uHtAng;uniform vec3 uHtInkCol;
float pxCellSize(){return max(2.,uPxCell*uRes.y);}
vec2 pxCenter(vec2 fc,float cs){vec2 cc=(floor(fc/cs)+.5)*cs;
 if(uPx==4){float ca=cos(uHtAng),sa=sin(uHtAng);vec2 rp=vec2(ca*fc.x+sa*fc.y,-sa*fc.x+ca*fc.y);
  vec2 rc=(floor(rp/cs)+.5)*cs;cc=vec2(ca*rc.x-sa*rc.y,sa*rc.x+ca*rc.y);}
 return cc;}
float pxDist(vec2 cc){vec2 dv=abs(HF_FC-cc);return uPx==3?max(dv.x,dv.y):length(dv);}
float pxRadius(float cs,float heat){return .5*cs*uPxDot*mix(1.,sqrt(heat),uPxHeat);}
float pxDotRadius(float cs,float cov){return .5*cs*uPxDot*1.3*sqrt(cov);}
float pxLum(vec3 c){return clamp(dot(c,vec3(.299,.587,.114)),0.,1.);}
float pxCover(float d,float r){return 1.-smoothstep(r-.75,r+.75,d);}
uniform sampler2D uGlyph;uniform float uGN,uGBri,uGCon,uGGam;uniform int uGCol,uGBg,uGRev,uGMono;
vec4 pxGlyph(vec2 cc,float cs,float lum){float L=clamp((lum-.5)*uGCon+.5+uGBri,0.,1.);L=pow(L,1./uGGam);if(uGRev==1)L=1.-L;
 float idx=min(floor(L*uGN),uGN-1.);vec2 lp=(HF_FC-cc)/(cs*max(uPxDot,.05))+.5;
 if(lp.x<0.||lp.x>1.||lp.y<0.||lp.y>1.)return vec4(0.);lp=clamp(lp,.002,.998);
 vec2 uv=(vec2(mod(idx,8.),floor(idx/8.))+vec2(lp.x,1.-lp.y))/vec2(8.,4.);return HF_TEX(uGlyph,uv);}
vec3 pxGlyphInk(vec3 c,vec4 gg,float lum){vec3 own=gg.rgb/max(gg.a,1e-4);vec3 ink=uGCol==0?own:c;if(uGBg==2&&uGCol==1)ink=lum>.5?c*.3:min(c*2.2+.2,1.);return ink;}

float h(vec2 p){p=fract(p*vec2(123.34,456.21));p+=dot(p,p+45.32);return fract(p.x*p.y);}
float n2(vec2 p){vec2 i=floor(p),f=fract(p);vec2 u=f*f*(3.-2.*f);
 return mix(mix(h(i),h(i+vec2(1,0)),u.x),mix(h(i+vec2(0,1)),h(i+vec2(1,1)),u.x),u.y);}
float fbm(vec2 p){float v=0.,a=.5;for(int i=0;i<5;i++){v+=a*n2(p);p=mat2(1.6,1.2,-1.2,1.6)*p;a*=.5;}return v;}
float fs(vec2 p){return .65*n2(p)+.35*n2(mat2(1.6,1.2,-1.2,1.6)*p+3.3);}
float waveX(vec2 p){return dot(p,uWDir)-uWPos+(fs(p*2.5+uSeed)-.5)*uWW*.8;}
float waveG(vec2 p){float x=waveX(p);return exp(-x*x/(uWW*uWW));}
float field(vec2 p){
 if(uAnim==1){float x=waveX(p);p-=uWDir*uWAmt*.9*x*exp(-x*x/(uWW*uWW));}
 vec2 c=uFlow*vec2(cos(uPh),sin(uPh));vec2 s=vec2(uSeed*7.13,uSeed*3.71);
 vec2 q=vec2(fs(p*uWarpScale+c+s),fs(p*uWarpScale-c.yx+s+5.2));
 vec2 w=p+(q-.5)*uWarp;
 vec2 r=vec2(fs(w*uWarpScale*1.6+q*2.+c.yx+s+1.7),fs(w*uWarpScale*1.6+q*2.-c+s+9.2));
 w+=(r-.5)*uWarp*.6;
 float f=uBase;
 int nc=1;if(uAnim==4){nc=uDepth==1?2:(uDepth==2?4:8);}
 for(int i=0;i<16;i++){ if(i>=uN)break;
  vec4 b=uB[i];vec4 st=uS[i];float k=float(i);
  vec2 pos=b.xy+uOrbit*vec2(cos(uPh+k*1.7),sin(2.*uPh+k*2.3)*.6);
  float rad=b.z*(1.+.06*sin(uPh+k*1.3));
  float cs=cos(st.z),sn=sin(st.z);mat2 rot=mat2(cs,-sn,sn,cs);
  vec2 ext=st.xy*rad;vec2 dl=rot*(w-pos);
  vec2 ax1=ext.x>=ext.y?vec2(1.,0.):vec2(0.,1.);vec2 ax2=vec2(1.)-ax1;
  float wf=1.;
  if(nc>1)wf*=mix(.5,.9,uSp1);if(nc>2)wf*=mix(.5,.9,uSp2);if(nc>4)wf*=mix(.5,.9,uSp3);
  for(int j=0;j<8;j++){ if(j>=nc)break;
   float fj=float(j);float s1=mod(fj,2.)<1.?-1.:1.;float s2=mod(floor(fj/2.),2.)<1.?-1.:1.;float s3=fj<4.?-1.:1.;
   vec2 e=ext;vec2 o=vec2(0.);
   if(nc>1){o+=ax1*s1*uSp1*uSplitSep*.55*dot(e,ax1);e*=vec2(1.)-ax1*.42*uSp1;}
   if(nc>2){o+=ax2*s2*uSp2*uSplitSep*.6*dot(e,ax2);e*=vec2(1.)-ax2*.4*uSp2;}
   if(nc>4){o+=ax1*s3*uSp3*uSplitSep*.55*dot(e,ax1);e*=vec2(1.)-ax1*.4*uSp3;}
   vec2 d=(dl-o)/e;
   float dd=length(d);
   f+=b.w*wf*(st.w>0.?exp(-(dd-1.)*(dd-1.)/(st.w*st.w)):(st.w<0.?exp(-abs(d.y)*2.2-d.x*d.x*1.4):exp(-dd*dd*1.6)));}}
 float rl=length(w-vec2(.02,-.03))-uRingR;f-=uRing*exp(-rl*rl/(uRingW*uRingW));
 f+=(fbm(w*uNoiseScale+c*.5+s.yx)-.5)*uNoise;
 return f;}
void main(){
 vec2 fc=HF_FC;float cs=pxCellSize();vec2 cc=pxCenter(fc,cs);
 if(uPx>0)fc=cc;
 vec2 uv=(fc-.5*uRes)/uRes.y;
 float f=field(uv);
 if(uAnim==3){f=uBase+(f-uBase)*(1.-uWAmt*waveG(uv));}
 vec2 gp=floor(fc*(1000./uRes.y)/uGrainSize);
 float lh=uAnim==2?uWAmt*.45*waveG(uv)*mix(.45,1.,smoothstep(uBase-.02,uBase+.18,f)):0.;
 // Keep the anti-banding dither under the Grain control: Grain 0 is clean.
 float gd=.006*min(1.,uGrain/.1);
 float t=clamp(.5+(f-.5)*uContrast+uOffset+lh+(h(gp+3.1)-.5)*gd,0.,1.);
 vec3 col=texture2D(uRamp,vec2(t*(511./512.)+.5/512.,.5)).rgb;
 col*=1.+(fbm(uv*3.+uSeed)-.5)*uPaper*.35;
 col*=1.-uVig*smoothstep(.35,1.05,length(uv*vec2(.85,1.)));
 vec3 base=col;
 col+=step(1.-uDust*.004,h(floor(fc*(1000./uRes.y)/1.6)+uSeed*13.))*.55;
 float tt=fract(uT*.37)*311.;
 float g=h(gp+tt);vec3 gc=vec3(h(gp+tt+17.),h(gp+tt+43.),h(gp+tt+71.));
 col+=(g-.5)*uGrain+(gc-.5)*uGrain*.3;
 float A=1.;
 if(uPx==5){
  float lum=pxLum(base);vec4 gg=pxGlyph(cc,cs,lum);vec3 ink=pxGlyphInk(col,gg,lum);vec3 bg=uGBg==2?col:vec3(0.);
  col=mix(bg,ink,gg.a);if(uGBg==1)A=gg.a;if(uGMono==1)col=vec3(pxLum(col));}
 else if(uPx>=2){
  float d=pxDist(cc);float r=pxRadius(cs,t);
  if(uPx==4){float L=pxLum(base);float cov=uHtTone==1?L:1.-L;r=pxDotRadius(cs,cov);if(uHtInk==1)col=uHtInkCol+(g-.5)*uGrain*.5;}
  float a=pxCover(d,r);
  vec3 gap=uPxGap==0?texture2D(uRamp,vec2(.5/512.,.5)).rgb:(uPxGap==3?uPaperCol:vec3(0.));
  col=mix(gap,col,a);if(uPxGap==2)A=a;}
 gl_FragColor=vec4(clamp(col,0.,1.),A);}`;

export const fieldFragment = `precision highp float;
const float FLOW=0.35,WARP=0.2,WARP_SCALE=1.6,NOISE=0.06,NOISE_SCALE=1.8;
float h(vec2 p){p=fract(p*vec2(123.34,456.21));p+=dot(p,p+45.32);return fract(p.x*p.y);}
float n2(vec2 p){vec2 i=floor(p),f=fract(p);vec2 u=f*f*(3.-2.*f);
 return mix(mix(h(i),h(i+vec2(1,0)),u.x),mix(h(i+vec2(0,1)),h(i+vec2(1,1)),u.x),u.y);}
float fbm(vec2 p){float v=0.,a=.5;for(int i=0;i<5;i++){v+=a*n2(p);p=mat2(1.6,1.2,-1.2,1.6)*p;a*=.5;}return v;}
float fs(vec2 p){return .65*n2(p)+.35*n2(mat2(1.6,1.2,-1.2,1.6)*p+3.3);}
vec2 drift(vec2 p,vec2 c,float amp){
 vec2 q=vec2(fs(p*WARP_SCALE+c),fs(p*WARP_SCALE-c.yx+5.2));
 vec2 r=vec2(fs(p*WARP_SCALE*1.6+q*2.+c.yx+1.7),fs(p*WARP_SCALE*1.6+q*2.-c+9.2));
 return p+((q-.5)+(r-.5)*.6)*WARP*amp;}
// Mirrored at the border, so the warp never smears the edge texels.
vec2 mirror(vec2 uv){return 1.-abs(1.-abs(uv));}

uniform vec2 uRes,uCover;uniform float uAspect,uPhase,uAmp;
uniform sampler2D uField,uRamp;
float decodeHeat(float u){if(u<=0.06)return 0.+(u-0.)*5.;if(u<=0.94)return 0.3+(u-0.06)*0.40909090909090917;if(u<=1.)return 0.66+(u-0.94)*5.666666666666661;return 1.;}
void main(){
 // Frame-height units from the field's centre, cropped like object-cover.
 vec2 p=(gl_FragCoord.xy/uRes-.5)*uCover*vec2(uAspect,1.);
 // Heatform's drift, then a little turbulence.
 vec2 c=FLOW*vec2(cos(uPhase),sin(uPhase));
 vec2 w=drift(p,c,uAmp);
 vec2 uv=mirror(vec2(w.x/uAspect,w.y)+.5);
 float f=decodeHeat(texture2D(uField,uv).r)+(fbm(w*NOISE_SCALE+c*.5)-.5)*NOISE*uAmp;
 // Heatform's dither keeps the 8-bit field from banding.
 float t=clamp(f+(h(gl_FragCoord.xy)-.5)*.006,0.,1.);
 gl_FragColor=vec4(texture2D(uRamp,vec2(t*0.998046875+0.0009765625,.5)).rgb,1.);
}`;
