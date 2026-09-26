import { test } from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { readSearchDOM } from './dom.mjs';

test('extracts only visible primary search cards; fails closed on wrong page/login',async()=>{
 const browser=await chromium.launch({channel:'chrome'});
 try {
  const page=await browser.newPage();
  const card=(id,text,extra='')=>`<div data-pressable-container="true"><a href="/@buyer/post/${id}"><time datetime="2026-09-26T10:00:00Z">1h</time></a><span dir="auto"><span>${text}</span><div role="button">Translate</div></span>${extra}</div>`;
  const primary=card('Ddv8MUkGt1S','Need a website for my shop',card('Ddv6qJyDc8V','Quoted unrelated post'));
  const html=`<a href="/@bekzhan_nurallin">Profile</a><input value="need a website"><div data-pagelet="threads_home_feed">${card('DdvxDHTDixq','Random feed')}</div><div data-pagelet="threads_search_results_0">${primary}</div><div data-pagelet="threads_search_results_1" style="display:none">${card('DdvzD4qiEyZ','Hidden post')}</div>`;
  await page.route('https://www.threads.com/**',r=>r.fulfill({contentType:'text/html',body:html}));
  await page.goto('https://www.threads.com/search?q=need+a+website&filter=recent');
  const args={query:'need a website',expectedUser:'bekzhan_nurallin'};
  const result=await page.evaluate(readSearchDOM,args);
  assert.equal(result.status,'ok');assert.equal(result.posts.length,1);assert.equal(result.posts[0].text,'Need a website for my shop');
  assert.equal((await page.evaluate(readSearchDOM,{...args,query:'different'})).status,'wrong_search');
  assert.equal((await page.evaluate(readSearchDOM,{...args,expectedUser:'other'})).status,'needs_login');
  await page.goto('https://www.threads.com/');
  assert.equal((await page.evaluate(readSearchDOM,args)).status,'wrong_search');
 } finally {await browser.close();}
});
