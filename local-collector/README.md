# Local Threads collector

The local browser reads rendered **Recent search result cards**, never anonymous SSR, home feeds, private messages, hidden GraphQL state or exported cookies. A dedicated Chrome profile stays on this computer. Chrome must be installed; Node.js 22+ is required.

Northflank retains Groq, Postgres and Telegram credentials. The local machine needs only an ingestion key that can submit public posts (it cannot read the database). The worker must run the new image with:

```
LEADWATCH_COLLECTION_MODE=ingest
LEADWATCH_INGEST_API_KEY=<same random 64-character hex key as local config>
```

Expose worker HTTP port 8080 with Northflank HTTPS termination only after the protected route is deployed. `/admin/posts` remains disabled unless a separate read key is explicitly configured. The existing main API service is not involved. No Meta API, replies, likes, auto-DMs, or outreach are used.

## Setup / operation

```
cd local-collector
npm ci
node collector.mjs init https://YOUR-WORKER.code.run/ingest/posts YOUR_THREADS_USERNAME
```

`init` generates a unique key directly into ignored `.state/config.json`; do not print, commit or share it in chat. Copy it securely to the worker secret configuration. Never copy your Threads cookies to Northflank.

- Double-click `start.cmd`: opens an independent Chrome profile, not your daily browser profile. Sign in once in that window. The collector waits for the expected account and resumes automatically after login.
- Double-click `report.cmd`: local audit of the last 50 query batches (candidates, **not** all qualified leads).
- Double-click `stop.cmd`: gracefully stops collection, usually within a few seconds (up to one navigation timeout).
- `npm run status`: safe status only; no keys.
- `node collector.mjs account USERNAME`: bind to the visibly verified test account while the collector is stopped; no password/cookie export.
- `npm run once`: one cycle, still observes persisted cooldown; waits for login if necessary.
- `npm test`: isolated DOM fixture and configuration tests; no live Threads traffic.

Every 15 minutes after completing a cycle it rotates through four queries, at most five fresh posts per query (20 candidates). A query is rechecked roughly every hour; full rotations include Russian and English software, websites, CRM, automation, integrations and bots. The computer must remain awake and the collector running. No Windows autostart or power-setting change is installed.

Only public permalink, text, post date, and source query are uploaded. Failed uploads remain in a local outbox and are retried before any further search. The worker deduplicates by post ID, rejects stale/future/off-topic posts, caps Groq attempts at 20 posts per 15 minutes, and sends qualified leads to the existing Telegram inbox. Old unverifiable records remain in the DB but are excluded from classification and notifications.

Login/checkpoint/CAPTCHA waits for a human; HTTP 429 causes a 60-minute cooldown. Other errors stop that cycle. There is no anti-bot evasion, fingerprint spoofing, proxy rotation or CAPTCHA solver. Browser automation can still be restricted by Threads and is not a guarantee against account limits.

`.state/` contains credentials and browser session material. It is excluded from Git **and** Docker context. Keep it private. To move computers, create a new independent login rather than uploading this folder.
