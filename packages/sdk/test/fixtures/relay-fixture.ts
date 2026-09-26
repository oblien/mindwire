import { execFile } from "node:child_process";
import { writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { promisify } from "node:util";

/** A real TLS/WebSocket relay on loopback, controlled without an outside service. */
export async function installRelayFixture(directory: string): Promise<string> {
  const certificate = join(directory, "relay-certificate.pem");
  await promisify(execFile)("openssl", ["req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
    "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1",
    "-keyout", join(directory, "relay-key.pem"), "-out", certificate]);
  const module = join(dirname(createRequire(import.meta.url).resolve("ws/package.json")), "index.js");
  const node = (await promisify(execFile)("node", ["-p", "process.execPath"])).stdout.trim();
  await writeFile(join(directory, "ngrok"), `#!${node}
const fs = require('node:fs');
const https = require('node:https');
const WebSocket = require(${JSON.stringify(module)});
const directory = process.env.MINDWIRE_RELAY_FIXTURE;
const origin = new URL(process.argv[3]);
const server = https.createServer({key:fs.readFileSync(directory+'/relay-key.pem'),cert:fs.readFileSync(directory+'/relay-certificate.pem')});
const websocket = new WebSocket.Server({noServer:true});
server.on('upgrade',(request,socket,head)=>{
  socket.on('error',()=>{});
  if(fs.existsSync(directory+'/relay-unavailable')) { socket.end('HTTP/1.1 503 Service Unavailable\\r\\nContent-Length: 0\\r\\nConnection: close\\r\\n\\r\\n'); return; }
  websocket.handleUpgrade(request,socket,head,client=>websocket.emit('connection',client));
});
websocket.on('connection',client=>{
  const upstream = new WebSocket('ws://127.0.0.1:'+origin.port+'/ssh');
  const pending=[];
  client.on('message',data=>{ if(upstream.readyState===WebSocket.OPEN) upstream.send(data); else pending.push(data); });
  upstream.on('open',()=>{ for(const data of pending) upstream.send(data); pending.length=0; });
  upstream.on('message',data=>{ if(client.readyState===WebSocket.OPEN) client.send(data); });
  upstream.on('error',()=>client.terminate()); client.on('error',()=>upstream.terminate());
  upstream.on('close',()=>client.terminate()); client.on('close',()=>upstream.terminate());
});
server.listen(0,'127.0.0.1',()=>{
  fs.appendFileSync(directory+'/relay-starts',process.pid+'\\n');
  console.log(JSON.stringify({msg:'started tunnel',url:'https://127.0.0.1:'+server.address().port}));
});
`, { mode: 0o700 });
  return certificate;
}
