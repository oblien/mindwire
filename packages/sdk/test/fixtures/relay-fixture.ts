import { execFile } from "node:child_process";
import { writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { promisify } from "node:util";

/** A real TLS/WebSocket relay on loopback, controlled without an outside service. */
export async function installRelayFixture(directory: string, options: { providerDNS?: boolean } = {}): Promise<string> {
  const certificate = join(directory, "relay-certificate.pem");
  await promisify(execFile)("openssl", ["req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
    "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1" + (options.providerDNS ? ",DNS:*.trycloudflare.com" : ""),
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
  console.log(JSON.stringify({msg:'started tunnel',url:'https://' + ${options.providerDNS ? "('relay-' + process.pid + '.trycloudflare.com')" : "'127.0.0.1'"} + ':'+server.address().port}));
});
// A provider writes again during network recovery, even after its controller exits.
setInterval(()=>console.error('provider heartbeat'),100);
`, { mode: 0o700 });
  return certificate;
}

/** A router returning NXDOMAIN while the provider still resolves a live tunnel.
 * Expiration is controlled per hostname, without any external DNS or TLS bypass. */
export async function installProviderDNSFixture(directory: string): Promise<string> {
  const file = join(directory, "provider-dns-fixture.mjs");
  await writeFile(file, `
import dns from 'node:dns';
import { Resolver } from 'node:dns/promises';
import { syncBuiltinESMExports } from 'node:module';
import { existsSync } from 'node:fs';
const directory = ${JSON.stringify(directory)};
const fixture = /^relay-\\d+\\.trycloudflare\\.com$/;
const missing = () => Object.assign(new Error('DNS fixture'), {code:'ENOTFOUND'});
const originalLookup = dns.lookup;
dns.lookup = function(hostname, options, callback) {
  if (!fixture.test(hostname)) return originalLookup.apply(this, arguments);
  const done = typeof options === 'function' ? options : callback;
  process.nextTick(() => done(missing(), '', 0));
};
for (const name of ['resolve4', 'resolve6']) {
  const original = Resolver.prototype[name];
  Resolver.prototype[name] = function(hostname) {
    return fixture.test(hostname) ? Promise.reject(missing()) : original.apply(this, arguments);
  };
}
syncBuiltinESMExports();
const originalFetch = globalThis.fetch;
globalThis.fetch = async function(input, options) {
  const url = new URL(input instanceof Request ? input.url : String(input));
  const hostname = url.searchParams.get('name');
  if (url.origin !== 'https://cloudflare-dns.com' || !fixture.test(hostname ?? '')) return originalFetch(input, options);
  const type = Number(url.searchParams.get('type'));
  const expired = existsSync(directory + '/expired-' + hostname);
  return new Response(JSON.stringify({Status:expired ? 3 : 0,Question:[{name:hostname,type}],
    Answer:!expired && type === 1 ? [{type:1,data:'127.0.0.1'}] : undefined}));
};
`, { mode: 0o600 });
  return file;
}
