// Runs in the page. Only rendered search cards, never hydration data or cookies.
export function readSearchDOM({ query, expectedUser }) {
  const visible = el => !!el && el.getClientRects().length > 0 && getComputedStyle(el).visibility !== 'hidden';
  const pageURL = new URL(location.href);
  const controls = Array.from(document.querySelectorAll('a,button,[role="button"]')).filter(visible);
  const blocked = controls.some(el => /^(log in|login|sign in|войти)$/i.test(el.innerText.trim())) || /\/login|\/accounts\//.test(pageURL.pathname);
  const challenge = /\/challenge|\/checkpoint/.test(pageURL.pathname) || Array.from(document.querySelectorAll('iframe')).some(el => /captcha/i.test(el.title + el.src));
  const profile = document.querySelector(`a[href="/@${expectedUser}"]`);
  const loggedIn = visible(profile) && !profile.closest('[data-pagelet^="threads_search_results_"]');
  if (challenge) return { status: 'challenge', posts: [] };
  if (blocked || !loggedIn) return { status: 'needs_login', posts: [] };
  if (pageURL.origin !== 'https://www.threads.com' || pageURL.pathname !== '/search' || pageURL.searchParams.get('q') !== query || pageURL.searchParams.get('filter') !== 'recent') return { status: 'wrong_search', posts: [] };
  const searchInput = Array.from(document.querySelectorAll('input')).find(el => visible(el) && el.value === query);
  if (!searchInput) return { status: 'wrong_search', posts: [] };
  const textOnly = node => {
    if (node.nodeType === Node.TEXT_NODE) return node.textContent;
    if (node.nodeType !== Node.ELEMENT_NODE || node.matches('button,[role="button"],time,svg,script,style,[aria-hidden="true"]') || !visible(node)) return '';
    if (node.tagName === 'BR') return '\n';
    return Array.from(node.childNodes).map(textOnly).join('');
  };
  const posts = [];
  const seen = new Set();
  const wrappers = Array.from(document.querySelectorAll('[data-pagelet^="threads_search_results_"]'));
  for (const wrapper of wrappers) {
    // One primary result per wrapper. Do not treat embedded/quoted posts as a new result.
    const clock = Array.from(wrapper.querySelectorAll('a[href*="/post/"] time[datetime]')).find(visible);
    const anchor = clock?.closest('a');
    const card = anchor?.closest('[data-pressable-container="true"]');
    if (!clock || !anchor || !card || !wrapper.contains(card)) continue;
    const link = new URL(anchor.getAttribute('href'), location.origin);
    if (link.origin !== location.origin || !/^\/@[A-Za-z0-9._]{1,40}\/post\/[A-Za-z0-9_-]{6,11}\/?$/.test(link.pathname)) continue;
    const candidates = Array.from(card.querySelectorAll('span[dir="auto"]'))
      .filter(el => visible(el) && !el.closest('a,button,[role="button"],time') && !el.querySelector('time') && el.closest('[data-pressable-container="true"]') === card)
      .filter(el => !el.parentElement.closest('span[dir="auto"]'))
      .map(el => textOnly(el).trim()).filter(text => text && !/^[\d\s.,]+[kKmM]?$/.test(text));
    // Captions may be several sibling spans (one per paragraph). Taking only
    // the longest loses buyer/seller context and can flip the classification.
    const text = candidates.join('\n');
    if (text.length < 8) continue;
    const permalink = link.origin + link.pathname.replace(/\/$/, '');
    if (seen.has(permalink)) continue;
    seen.add(permalink);
    posts.push({ permalink, text, posted_at: clock.getAttribute('datetime') });
  }
  if (wrappers.length && !posts.length) return { status: 'markup_changed', posts: [] };
  if (!wrappers.length) {
    const empty = /no results|nothing found|ничего не найдено|нет результатов/i.test(document.body.innerText);
    return { status: empty ? 'empty' : 'loading', posts: [] };
  }
  return { status: 'ok', posts };
}
