import { test } from 'node:test';
import assert from 'node:assert/strict';
import { freshPosts, selectQueries, validateConfig, renderReport, matchesTopic } from './core.mjs';
test('intent words alone do not consume the candidate budget',()=>{
 assert.equal(matchesTopic('ищу разработчика','Ищу дом для котёнка'),false);
 assert.equal(matchesTopic('ищу разработчика','Нужен программист для проекта'),true);
 assert.equal(matchesTopic('looking for CRM','Need Kommo integration'),true);
});
test('rotation and bounded fresh deduplication',()=>{
  assert.deepEqual(selectQueries(['a','b','c'],2),['c','a','b']);
  const p={permalink:'https://www.threads.com/@buyer/post/Ddv8MUkGt1S',text:'Need a website',posted_at:new Date().toISOString()};
  assert.equal(freshPosts([p,p,{...p,permalink:'x',posted_at:'invalid'},{...p,permalink:'y',posted_at:'2000-01-01'}]).length,1);
  const now=Date.now();
  assert.equal(freshPosts([{...p,posted_at:new Date(now-47*60*60*1000).toISOString()}],now).length,1);
  assert.equal(freshPosts([{...p,posted_at:new Date(now-49*60*60*1000).toISOString()}],now).length,0);
  assert.equal(freshPosts(Array.from({length:20},(_,i)=>({...p,permalink:String(i)}))).length,5);
});
test('only secure explicit endpoints and conservative schedule',()=>{
  const cfg={endpoint:'https://example.com/ingest/posts',key:'a'.repeat(64),expectedUser:'bekzhan_nurallin',queries:['need a website'],intervalMinutes:15};
  assert.equal(validateConfig(cfg),cfg);
  for(const endpoint of ['http://example.com/ingest/posts','https://user:pw@example.com/ingest/posts','https://example.com/ingest/posts?key=x','https://example.com/admin/posts']) assert.throws(()=>validateConfig({...cfg,endpoint}));
  assert.throws(()=>validateConfig({...cfg,intervalMinutes:5}));
});
test('audit escapes untrusted posts, never includes key',()=>{
  const html=renderReport({state:'waiting',key:'TOP_SECRET'},[{query:'<script>evil()</script>',posts:[{text:'<img onerror=alert(1)>',permalink:'https://www.threads.com/@buyer/post/Ddv8MUkGt1S'}]}]);
  assert.ok(!html.includes('<script>'));assert.ok(!html.includes('<img'));assert.ok(!html.includes('TOP_SECRET'));
});
