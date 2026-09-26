import { chromium } from 'playwright';
import { readFile, writeFile, mkdir, rename, unlink, open, access } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as sleep } from 'node:timers/promises';
import { DEFAULT_QUERIES, validateConfig, selectQueries, freshPosts, renderReport } from './core.mjs';
import { readSearchDOM } from './dom.mjs';

const root = dirname(fileURLToPath(import.meta.url));
const stateDir = join(root, '.state');
const path = name => join(stateDir, name);
await mkdir(stateDir, { recursive: true });
async function read(name, fallback) {
  try { return JSON.parse(await readFile(path(name), 'utf8')); }
  catch (e) { if (e.code === 'ENOENT') return fallback; throw new Error('invalid_local_state'); }
}
async function save(name, value) {
  await writeFile(path(name+'.tmp'), JSON.stringify(value,null,2), { mode:0o600 });
  await rename(path(name+'.tmp'), path(name));
}
async function exists(name) { try { await access(path(name)); return true; } catch { return false; } }
const command = process.argv[2] ?? 'start';
if (command === 'init') {
  if (await exists('config.json')) throw new Error('config_already_exists');
  const cfg = validateConfig({ endpoint: process.argv[3], key:randomBytes(32).toString('hex'), expectedUser:process.argv[4], intervalMinutes:15, queries:DEFAULT_QUERIES });
  await save('config.json',cfg);
  console.log('Local config created. Key stays in .state/config.json; never commit or paste it in chat.');
  process.exit(0);
}
if (command === 'status') { console.log(JSON.stringify(await read('status.json',{ state:'not_started' }),null,2)); process.exit(0); }
if (command === 'stop') { await writeFile(path('stop'),'stop'); console.log('Stop requested.'); process.exit(0); }
if (!['start','once'].includes(command)) throw new Error('unknown_command');
const cfg = validateConfig(await read('config.json',null));
// Stale locks may be cleared only when their exact recorded process no longer exists.
const previous = await read('lock.json',null);
if (previous) {
  try { process.kill(previous.pid,0); throw new Error('collector_already_running'); }
  catch (e) { if (e.code !== 'ESRCH') throw e; await unlink(path('lock.json')); }
}
const lock = await open(path('lock.json'),'wx',0o600);
await lock.writeFile(JSON.stringify({pid:process.pid}));
await lock.close();
if (await exists('stop')) await unlink(path('stop'));
let context;
let stopped = false;
let audit = await read('audit.json',[]);
let runtime = await read('runtime.json',{ cursor:0, outbox:[], nextRun:0 });
let status = {};
async function update(state, extra={}) {
  status = { state, pid:process.pid, updatedAt:new Date().toISOString(), ...extra };
  await save('status.json',status);
  await writeFile(path('report.html'),renderReport(status,audit));
  console.log(JSON.stringify(status));
}
async function wait(ms) {
  const until = Date.now()+ms;
  while (!stopped && Date.now()<until) {
    if (await exists('stop')) { stopped=true; break; }
    await sleep(Math.min(1000,until-Date.now()));
  }
}
async function flush() {
  for (const batch of [...runtime.outbox]) {
    let response;
    try {
      response = await fetch(cfg.endpoint,{method:'POST',headers:{Authorization:`Bearer ${cfg.key}`,'Content-Type':'application/json'},body:JSON.stringify(batch),redirect:'error',signal:AbortSignal.timeout(20000)});
    } catch { throw new Error('upload_network_error'); }
    if (!response.ok) throw new Error(`upload_http_${response.status}`);
    const result = await response.json();
    if (!Number.isInteger(result.stored) || !Number.isInteger(result.accepted)) throw new Error('upload_invalid_response');
    runtime.outbox.shift();
    await save('runtime.json',runtime);
    audit.push({ at:new Date().toISOString(), query:batch.query, posts:batch.posts, status:`uploaded: accepted=${result.accepted}, new=${result.stored}, rejected=${result.rejected}` });
    audit=audit.slice(-50);
    await save('audit.json',audit);
    await update('uploaded',{query:batch.query,...result});
  }
}
async function collect(page,query) {
  const url = new URL('https://www.threads.com/search');
  url.searchParams.set('q',query); url.searchParams.set('filter','recent');
  const response = await page.goto(url.href,{waitUntil:'domcontentloaded',timeout:45000});
  if (response?.status()===429) throw new Error('threads_rate_limited');
  if (response && response.status()>=400) throw new Error('threads_http_error');
  let found;
  // Allow hydration; no repeated network navigation while a login/challenge is displayed.
  for (let i=0;i<15;i++) {
    found = await page.evaluate(readSearchDOM,{query,expectedUser:cfg.expectedUser});
    if (['ok','empty','challenge'].includes(found.status)) break;
    await wait(1000); if(stopped) return null;
  }
  if (['needs_login','challenge'].includes(found.status)) {
    await update(found.status,{query});
    // Human login stays local. On success, restart the exact search once.
    while (!stopped) {
      await wait(10000); if(stopped) return null;
      const current = await page.evaluate(readSearchDOM,{query,expectedUser:cfg.expectedUser});
      if (!['needs_login','challenge'].includes(current.status)) return collect(page,query);
    }
  }
  if (stopped) return null;
  if (!['ok','empty'].includes(found.status)) throw new Error(`threads_${found.status}`);
  const candidates = [...found.posts];
  // A bounded scroll only, no infinite harvesting and no clicks/replies/likes.
  for(let i=0; i<2 && freshPosts(candidates).length<5 && found.status!=='empty'; i++) {
    await page.mouse.wheel(0,700);
    await wait(2000); if(stopped) return null;
    found=await page.evaluate(readSearchDOM,{query,expectedUser:cfg.expectedUser});
    if(!['ok','empty'].includes(found.status)) throw new Error(`threads_${found.status}`);
    candidates.push(...found.posts);
  }
  return { query,source_url:url.href,posts:freshPosts(candidates) };
}
for(const signal of ['SIGINT','SIGTERM']) process.on(signal,()=>{stopped=true;});
try {
  context=await chromium.launchPersistentContext(path('browser-profile'),{channel:'chrome',headless:false,viewport:{width:1280,height:900},acceptDownloads:false});
  context.on('close',()=>{stopped=true;});
  const page=context.pages()[0] ?? await context.newPage();
  // Keep a visible page so a human can log in when required. No profile export.
  await update('starting');
  do {
    if(runtime.nextRun>Date.now()) {
      await update('waiting',{nextRun:new Date(runtime.nextRun).toISOString()});
      await wait(runtime.nextRun-Date.now()); if(stopped) break;
    }
    try {
      await flush();
      for(const query of selectQueries(cfg.queries,runtime.cursor)) {
        if(stopped) break;
        await update('searching',{query});
        const batch=await collect(page,query);
        if(!batch || stopped) break;
        runtime.outbox.push(batch);
        runtime.cursor=(runtime.cursor+1)%cfg.queries.length;
        await save('runtime.json',runtime);
        await flush();
        await wait(15000);
      }
      runtime.nextRun=Date.now()+cfg.intervalMinutes*60000;
    } catch(e) {
      // Never log raw Playwright/fetch exceptions (can contain page or header data).
      const safe=/^(threads_|upload_)[a-z_0-9]+$/.test(e.message) ? e.message : 'collector_error';
      runtime.nextRun=Date.now()+(safe==='threads_rate_limited'?60:cfg.intervalMinutes)*60000;
      await update(safe,{nextRun:new Date(runtime.nextRun).toISOString()});
      if(command==='once') process.exitCode=1;
    }
    await save('runtime.json',runtime);
    if(command==='once') break;
  } while(!stopped);
} catch { await update('browser_start_or_runtime_error'); process.exitCode=1; }
finally {
  await context?.close().catch(()=>{});
  await unlink(path('lock.json')).catch(()=>{});
  await update('stopped');
}
