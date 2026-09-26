import { test } from 'node:test';
import assert from 'node:assert/strict';
import { makeLookup, uploadBatch } from './upload.mjs';
const run=(fn,all)=>new Promise((resolve,reject)=>fn('worker.example',{all},(e,address,family)=>e?reject(e):resolve({address,family})));
test('DNS fallback is restricted to resolution errors and preserves callback shape',async()=>{
 const missing=Object.assign(new Error(),{code:'ENOTFOUND'});
 const fallback=makeLookup(async()=>{throw missing},async()=>['192.0.2.1']);
 assert.deepEqual(await run(fallback,false),{address:'192.0.2.1',family:4});
 assert.deepEqual((await run(fallback,true)).address,[{address:'192.0.2.1',family:4}]);
 const system=makeLookup(async()=>[{address:'192.0.2.2',family:4}],()=>assert.fail('should not fallback'));
 assert.equal((await run(system,false)).address,'192.0.2.2');
 const denied=makeLookup(async()=>{throw Object.assign(new Error(),{code:'EACCES'})},()=>assert.fail('unexpected fallback'));
 await assert.rejects(run(denied,false));
});
test('upload requires HTTPS',async()=>{
 await assert.rejects(uploadBatch('http://example.com/ingest/posts','unused',{}),/invalid_endpoint/);
});
