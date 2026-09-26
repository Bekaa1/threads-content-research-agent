import https from 'node:https';
import { lookup, Resolver } from 'node:dns/promises';

const publicDNS = new Resolver({timeout:3000,tries:1});
publicDNS.setServers(['1.1.1.1']);
// Some local resolvers retain NXDOMAIN after a new code.run port is exposed.
// Fallback only for DNS failure. TLS hostname/certificate validation stays on.
export function makeLookup(systemLookup=lookup, resolve4=host=>publicDNS.resolve4(host)) {
  return (hostname, options, callback) => {
    const all=typeof options==='object' && options.all;
    systemLookup(hostname,{all:true}).then(
      addresses=>all?callback(null,addresses):callback(null,addresses[0].address,addresses[0].family),
      async error=>{
        if(!['ENOTFOUND','EAI_AGAIN'].includes(error.code)) {callback(error);return;}
        try {
          const addresses=(await resolve4(hostname)).map(address=>({address,family:4}));
          if(!addresses.length) {callback(error);return;}
          if(all) callback(null,addresses); else callback(null,addresses[0].address,4);
        } catch {callback(error);}
      }
    );
  };
}
const resolveHost=makeLookup();
export async function uploadBatch(endpoint,key,batch) {
  const url=new URL(endpoint);
  if(url.protocol!=='https:' || url.username || url.password) throw new Error('upload_invalid_endpoint');
  const body=JSON.stringify(batch);
  return new Promise((resolve,reject)=>{
    const req=https.request(url,{method:'POST',lookup:resolveHost,headers:{Authorization:`Bearer ${key}`,'Content-Type':'application/json','Content-Length':Buffer.byteLength(body)}},res=>{
      let data='';
      res.setEncoding('utf8');
      res.on('data',chunk=>{data+=chunk;if(data.length>65536) req.destroy(new Error('upload_response_too_large'));});
      res.on('error',()=>reject(new Error('upload_network_error')));
      res.on('end',()=>{
        clearTimeout(timer);
        if(res.statusCode<200 || res.statusCode>=300) {reject(new Error(`upload_http_${res.statusCode}`));return;}
        try {resolve(JSON.parse(data));} catch {reject(new Error('upload_invalid_response'));}
      });
    });
    const timer=setTimeout(()=>req.destroy(new Error('upload_timeout')),20000);
    req.on('error',()=>{clearTimeout(timer);reject(new Error('upload_network_error'));});
    // No redirect following: credentials never leave the configured hostname.
    req.end(body);
  });
}
