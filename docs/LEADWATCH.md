# Leadwatch worker

`cmd/leadwatch` is a separate, single-replica worker for finding public Threads posts that look like active requests for software or automation services. It does not use the Meta OAuth/API service in the other project and always clears Threads session/CSRF values before searching. Collection uses this repository's undocumented public-web scraper, so availability can change and deployment must comply with Threads' current terms and access limits.

The worker searches eight RU/EN phrases hourly by default, stores post IDs and classifications in the existing Postgres database table `leadwatch_posts`, asks Groq to classify small batches, then sends only high-confidence matches to Telegram. The Telegram message includes the source post, reason, and an optional draft. It never sends a Threads reply or DM; the user reviews and sends any outreach manually. Posts are treated as untrusted prompt input, and the classifier is told not to follow embedded instructions or invent experience, prices, or results.

## Northflank configuration

Create a separate service using this repository and branch, with Dockerfile path `Dockerfile.leadwatch`. Keep the existing API service unchanged and set the worker to one replica. The container listens on `PORT` (default `8080`) and serves `GET /healthz`.

Required environment variables (store credentials as secrets):

- `DATABASE_URL`: connection string from the existing `threads-lead-db` addon, injected by a Northflank secret/reference rather than copied into source control. The worker creates only its own `leadwatch_posts` table.
- `GROQ_API_KEY`: Groq API key.
- `TELEGRAM_BOT_TOKEN`: bot token; rotate any token that has been pasted into a chat or other public place.
- `TELEGRAM_CHAT_ID`: destination group/chat ID.

Optional settings:

- `GROQ_MODEL` (default `openai/gpt-oss-20b`; change only to a model currently enabled in your Groq account)
- `LEADS_INTERVAL_MINUTES` (default `60`, minimum `15`)
- `LEADS_QUERIES` (queries separated by `|` or newlines)
- `LEADS_OFFER_PROFILE` (short factual service description used only to judge relevance; avoid private customer data)

Search and classification are capped per cycle to limit scraping and model usage. This is a discovery aid, not a guarantee of complete/recent results: logged-out search is unofficial and may omit posts or stop working when Threads changes its web surface.
