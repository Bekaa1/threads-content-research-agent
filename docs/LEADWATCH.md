# Leadwatch worker

`cmd/leadwatch` is a separate, single-replica worker for finding public Threads posts that look like active requests for software or automation services. It does not use the Meta OAuth/API service in the other project and always clears Threads session/CSRF values before searching. Collection uses this repository's undocumented public-web scraper, so availability can change and deployment must comply with Threads' current terms and access limits.

The worker searches eight RU/EN phrases with a 15-minute delay between cycles by default, stores post IDs and classifications in the existing Postgres database table `leadwatch_posts`, asks Groq to classify small batches, then sends only high-confidence matches to Telegram. The Telegram message includes the source post, query, source parser, post date, reason, and an optional draft. It never sends a Threads reply or DM; the user reviews and sends any outreach manually. Posts are treated as untrusted prompt input, and the classifier is told not to follow embedded instructions or invent experience, prices, or results.

## Northflank configuration

Create a separate service using this repository and branch, with Dockerfile path `Dockerfile.leadwatch`. Keep the existing API service unchanged and set the worker to one replica. The container listens on `PORT` (default `8080`) and serves `GET /healthz`.

Required environment variables (store credentials as secrets):

- `DATABASE_URL`: connection string from the existing `threads-lead-db` addon, injected by a Northflank secret/reference rather than copied into source control. The worker creates only its own `leadwatch_posts` table.
- `GROQ_API_KEY`: Groq API key.
- `TELEGRAM_BOT_TOKEN`: bot token; rotate any token that has been pasted into a chat or other public place.
- `TELEGRAM_CHAT_ID`: destination group/chat ID.

Optional settings:

- `GROQ_MODEL` (default `openai/gpt-oss-20b`; change only to a model currently enabled in your Groq account)
- `LEADWATCH_INTERVAL_MINUTES` (default `15`, range `15`–`1440`; the old `LEADS_INTERVAL_MINUTES` name is ignored)
- `LEADS_QUERIES` (queries separated by `|` or newlines)
- `LEADS_OFFER_PROFILE` (short factual service description used only to judge relevance; avoid private customer data)

Search and classification are capped per cycle to limit scraping and model usage. This is a discovery aid, not a guarantee of complete/recent results: logged-out search is unofficial and may omit posts or stop working when Threads changes its web surface.

## Search integrity

- Both HTML and GraphQL paths accept only a recognized `searchResults.edges[].node.thread.thread_items` connection. Home/recommended feed objects are not keyword results, even when the request contained a keyword.
- Explicitly mismatched SSR query echoes, unexpected result shapes, HTTP errors, and GraphQL error envelopes are failures, not empty successful searches.
- Valid empty search connections are logged as `empty`. This does **not** prove no matching posts exist on Threads; anonymous coverage can be incomplete.
- HTTP access/rate-limit failures do not trigger a fallback request. A 429 stops remaining queries and doubles the inter-cycle delay, capped at one hour (or the configured delay, if larger). Pagination respects the requested post limit before making further calls.
- Each new row stores `source` and `source_url`. Old rows remain intact with an empty source and are excluded from classification/notification queues until seen again in a verified search connection. Previously sent notifications are never reset.
- Query logs include `status` and `verified_posts`; cycle logs distinguish `ok`, `empty`, `degraded`, and `unavailable`, with succeeded/failed/skipped query counts. `/healthz` means the process is alive, not that search is returning usable results.
- The protected `/admin/posts` endpoint includes provenance on the last five stored posts. An empty `source` explicitly identifies a legacy, unverified record.

Structural verification is not an intent classifier. Groq still decides whether the author is requesting a service rather than advertising one. Do not loosen these checks merely to increase post counts.

A second topic sanity check is required: live testing showed unrelated posts even inside a correctly shaped anonymous `searchResults` connection with the right query echo. The worker requires a service term or alias in the post before storing it or calling Groq. It does not require the entire query phrase, and it does not treat an advertiser as a buyer. All-off-topic responses are logged as `off_topic` query failures, not successful empty searches. This heuristic can miss implicit or image-only requests; it is a fail-safe, not a repair of Threads' anonymous search coverage. Logged-in browser search must be evaluated separately before choosing a replacement collector; this change does not export or use account sessions.

## Verification

Run `go test ./...`. The Postgres integration test is skipped unless `LEADWATCH_TEST_DATABASE_URL` targets exactly `postgres://postgres@127.0.0.1:5432/leadwatch_test?sslmode=disable`. Use a new disposable PostgreSQL 18 container with `--network none`, database `leadwatch_test`, and trust auth; run the Go test container with `--network container:<test-db-container>` so only those containers share the database loopback. Never use production credentials. The test covers migration from the old schema, legacy quarantine, provenance round-trips, deduplication, and notification state preservation.
