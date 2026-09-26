export const DEFAULT_QUERIES = [
  'ищу разработчика', 'looking for web developer',
  'нужен сайт', 'need a website',
  'ищу интегратора', 'looking for CRM',
  'нужен бот', 'need an automation expert',
  'amoCRM', 'n8n',
  'разработать приложение', 'need an app developer',
  'автоматизация бизнеса', 'looking for chatbot developer',
  'Битрикс', 'Kommo'
];

export function validateConfig(cfg) {
  const endpoint = new URL(cfg.endpoint);
  if (endpoint.protocol !== 'https:' || endpoint.username || endpoint.password || endpoint.search || endpoint.hash || endpoint.pathname !== '/ingest/posts') throw new Error('invalid_endpoint');
  if (!/^[a-f0-9]{64}$/.test(cfg.key)) throw new Error('invalid_ingest_key');
  if (!/^[A-Za-z0-9._]{1,40}$/.test(cfg.expectedUser)) throw new Error('invalid_expected_user');
  if (!Array.isArray(cfg.queries) || !cfg.queries.length || cfg.queries.length > 50 || cfg.queries.some(q => typeof q !== 'string' || q.trim().length < 2 || q.length > 200)) throw new Error('invalid_queries');
  if (!Number.isInteger(cfg.intervalMinutes) || cfg.intervalMinutes < 15 || cfg.intervalMinutes > 1440) throw new Error('invalid_interval');
  return cfg;
}

export function selectQueries(queries, cursor, count = 4) {
  return Array.from({ length: Math.min(count, queries.length) }, (_,i) => queries[(cursor+i) % queries.length]);
}

const topicAliases = [
  ['сайт','лендинг','веб','верст','вёрст','website','web','site','landing','wordpress','shopify'],
  ['разработ','программ','developer','programmer','coder','coding','software','engineer'],
  ['crm','срм','амосрм','amocrm','kommo','коммо','битрикс','bitrix','воронк'],
  ['автоматиз','automation','automate','automating','workflow','n8n'],
  ['интегратор','интеграц','integrator','integration','integrate','integrating','api'],
  ['бот','бота','ботов','чатбот','bot','bots','chatbot','assistant','ассистент'],
  ['приложен','мобильн','app','apps','mobile','reactnative']
];
export function matchesTopic(query,text) {
  const words = s => s.toLowerCase().split(/[^\p{L}\p{N}]+/u);
  const contains = (ws, aliases) => ws.some(w=>aliases.some(a=>w===a || (a.length>=4 && w.startsWith(a))));
  const topics=topicAliases.filter(aliases=>contains(words(query),aliases));
  // Unknown custom queries are still validated by the server's stricter check.
  return !topics.length || topics.some(aliases=>contains(words(text),aliases));
}

export function freshPosts(posts, now = Date.now(), query = '') {
  const seen = new Set();
  return posts.filter(p => {
    const date = Date.parse(p.posted_at);
    if (!Number.isFinite(date) || date > now + 300000 || date < now - 30*86400000 || !p.text?.trim() || p.text.length > 10000 || seen.has(p.permalink) || !matchesTopic(query,p.text)) return false;
    seen.add(p.permalink);
    return true;
  }).slice(0,5); // Four queries x five posts: at most 20 candidate posts per cycle.
}

export function escapeHTML(value) {
  return String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}

export function renderReport(status, audit) {
  return `<!doctype html><html lang="ru"><meta charset="utf-8"><meta http-equiv="refresh" content="30"><meta name="viewport" content="width=device-width"><title>Threads — журнал поиска</title>
  <style>body{font:16px system-ui;background:#101820;color:#ecf4f7;max-width:1000px;margin:40px auto;padding:20px}a{color:#76d4fd}article{border:1px solid #3c4a54;border-radius:12px;padding:18px;margin:14px 0}small{color:#aebcc7}pre{white-space:pre-wrap}h1{font-size:26px}</style>
  <h1>Threads — журнал локального поиска</h1><p>Состояние: <b>${escapeHTML(status.state)}</b> · ${escapeHTML(status.updatedAt)}</p><p>Следующий цикл: ${escapeHTML(status.nextRun ?? '—')}</p>
  <p>Это просмотренные кандидаты, <b>не все являются лидами</b>. Покупательский запрос проверяет Groq в Northflank. В Telegram уходят только подходящие.</p>
  ${audit.slice(-50).reverse().map(batch => `<article><h2>${escapeHTML(batch.query)}</h2><small>${escapeHTML(batch.at)} · ${escapeHTML(batch.status)} · ${escapeHTML(batch.posts.length)} постов</small>${batch.posts.map(p=>`<p><a target="_blank" rel="noopener noreferrer" href="${escapeHTML(p.permalink)}">${escapeHTML(p.permalink)}</a> · ${escapeHTML(p.posted_at)}</p><pre>${escapeHTML(p.text)}</pre>`).join('')}</article>`).join('') || '<p>Пока нет результатов. Нужен вход в отдельном окне Threads.</p>'}</html>`;
}
