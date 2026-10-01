import {readFileSync} from 'node:fs';

// Development and preview web edge, the same contract as the production edge
// in docs/HOSTING.md: /api reaches Agent, and every shard whose catalog
// publicTransportUrl is a route reaches its GameWorld through this origin.

// Each route serves <route>/transport/* (socket and references) from the
// shard's own transportUrl, so pages dial their origin from any host or scheme.
export function shardRoutes(catalogFile,relay=relaySameOrigin){
 let catalog;
 try{catalog=JSON.parse(readFileSync(catalogFile,'utf8'));}
 catch(cause){throw Error(`Shard catalog ${catalogFile} is unreadable; set SRO_SHARD_CATALOG`,{cause});}
 return Object.fromEntries(catalog.shards.filter(shard=>shard.publicTransportUrl?.startsWith('/')).map(({publicTransportUrl:route,transportUrl:target})=>[`${route}/transport`,{
  target,ws:true,changeOrigin:true,
  rewrite:path=>path.slice(route.length),
  configure:relay
 }]));
}

// The edge vouches for its own pages: a request whose Origin is this server
// reaches loopback-only upstream policies as a local caller, without Origin.
// Any other Origin is forwarded untouched and meets the upstream allowlist.
// tunnelHosts are public names a TLS-terminating tunnel serves this edge on.
export function sameOriginRelay(tunnelHosts=[]){
 const tunneled=new Set(tunnelHosts.map(host=>host.toLowerCase()));
 return proxy=>{
  const relay=(proxyReq,req)=>{const origin=req.headers.origin;if(origin&&origin.toLowerCase()===ownOrigin(req,tunneled))proxyReq.removeHeader('origin');};
  proxy.on('proxyReq',relay);
  proxy.on('proxyReqWs',relay);
 };
}
export const relaySameOrigin=sameOriginRelay();

// SRO_DEV_TUNNEL_HOSTS: comma-separated host names (no scheme or port) that a
// tunnel such as Cloudflare Tunnel or ngrok forwards to this edge.
export function tunnelHosts(env){
 return (env.SRO_DEV_TUNNEL_HOSTS??'').split(',').map(host=>host.trim().toLowerCase()).filter(Boolean);
}

// HTTP/2 names the authority in pseudo-headers; HTTP/1.1 and upgrades use Host.
// A tunnel terminates TLS and forwards plain HTTP, so its X-Forwarded-Proto
// names the page's scheme; it is believed only for a declared tunnel host.
function ownOrigin(req,tunneled){
 const host=req.headers[':authority']??req.headers.host;
 if(!host)return null;
 const forwarded=tunneled.has(host.toLowerCase().replace(/:\d+$/,''))&&
  String(req.headers['x-forwarded-proto']??'').split(',')[0].trim().toLowerCase()==='https';
 const scheme=req.headers[':scheme']??(req.socket.encrypted||forwarded?'https':'http');
 return `${scheme}://${host}`.toLowerCase();
}

// `pnpm dev:https` certificate: an explicit pair (mkcert, trusted wherever its
// CA is installed), or null for @vitejs/plugin-basic-ssl's self-signed one.
export function tlsCertificate(env){
 const {SRO_DEV_TLS_CERT:cert,SRO_DEV_TLS_KEY:key}=env;
 if(!cert&&!key)return null;
 if(!cert||!key)throw Error('Set both SRO_DEV_TLS_CERT and SRO_DEV_TLS_KEY');
 return {cert:readFileSync(cert),key:readFileSync(key)};
}
