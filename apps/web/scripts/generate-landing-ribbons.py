"""Original projected ribbon sculptures. Native SVG assets, no stock artwork."""
from math import sin, cos, pi, sqrt
from pathlib import Path

import sys
OUT = Path(sys.argv[1] if len(sys.argv) > 1 else '.cache/mindwire-ribbons')
OUT.mkdir(parents=True, exist_ok=True)

def add(a,b): return tuple(x+y for x,y in zip(a,b))
def mul(a,n): return tuple(x*n for x in a)
def dot(a,b): return sum(x*y for x,y in zip(a,b))
def cross(a,b): return (a[1]*b[2]-a[2]*b[1],a[2]*b[0]-a[0]*b[2],a[0]*b[1]-a[1]*b[0])
def unit(a): return mul(a,1/sqrt(dot(a,a)))
def mix(a,b,t): return tuple(x+(y-x)*t for x,y in zip(a,b))

def rotate(p):
    x,y,z=p
    a=.56
    x,z=x*cos(a)+z*sin(a),-x*sin(a)+z*cos(a)
    b=-.57
    y,z=y*cos(b)-z*sin(b),y*sin(b)+z*cos(b)
    c=-.38
    return (x*cos(c)-y*sin(c),x*sin(c)+y*cos(c),z)

def center(t,kind):
    if kind=='loop': return (2.0*cos(t),1.65*sin(t),.3*sin(2*t))
    return ((2+.63*cos(3*t))*cos(2*t),(2+.63*cos(3*t))*sin(2*t),.9*sin(3*t))

def surface(t,v,kind):
    c=center(t,kind)
    tangent=unit(add(center(t+.0001,kind),mul(center(t-.0001,kind),-1)))
    normal=unit(cross(tangent,(0,0,1)))
    binormal=unit(cross(tangent,normal))
    twist=.5*t+(0 if kind=='loop' else .4*sin(3*t))
    across=add(mul(normal,cos(twist)),mul(binormal,sin(twist)))
    thick=unit(cross(tangent,across))
    return rotate(add(c,add(mul(across,.49*cos(v)),mul(thick,.095*sin(v)))))

def generate(kind):
    faces=[]
    segments=640 if kind=='knot' else 480
    strips=80
    light=unit((-.65,-.8,1.7))
    half=unit(add(light,(0,0,1)))
    for i in range(segments):
      t=2*pi*i/segments
      tn=2*pi*(i+1)/segments
      for j in range(strips):
        v=2*pi*j/strips;vn=2*pi*(j+1)/strips
        points=[surface(t,v,kind),surface(tn,v,kind),surface(tn,vn,kind),surface(t,vn,kind)]
        normal=unit(cross(add(points[1],mul(points[0],-1)),add(points[3],mul(points[0],-1))))
        if normal[2]<0: normal=mul(normal,-1)
        diffuse=max(0,dot(normal,light))
        spec=max(0,dot(normal,half))**35
        colors=[(239,87,37),(247,155,70),(250,211,102),(204,193,218),(76,103,213),(33,63,173),(173,160,212),(239,87,37)]
        f=(t/(2*pi))*7;k=min(6,int(f));u=f-k;u=u*u*(3-2*u)
        base=mix(colors[k],colors[k+1],u)
        color=[min(255,round(c*(.53+.47*diffuse)+90*spec)) for c in base]
        depth=sum(p[2] for p in points)/4
        def project(p):
          scale=107*(1+p[2]*.035)
          return (400+p[0]*scale,400+p[1]*scale)
        q=[project(p) for p in points]
        path='M'+'L'.join(f'{x:.2f},{y:.2f}' for x,y in q)+'Z'
        fill='#'+''.join(f'{c:02x}' for c in color)
        faces.append((depth,f'<path d="{path}" fill="{fill}" stroke="{fill}" stroke-width=".55" stroke-linejoin="round"/>'))
    faces.sort(key=lambda f:f[0])
    svg='<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 800"><title>Original interwoven ribbon sculpture</title>' + ''.join(f[1] for f in faces)+'</svg>'
    (OUT/f'ribbon-{kind}.svg').write_text(svg)

for k in ['knot','loop']: generate(k)
