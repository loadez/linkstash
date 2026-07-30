# Backlog

Small, independent, PR-sized tasks. Each touches one service. Grab one, branch
from `main`, open a PR — CI runs the focused subset for the service you touch.

1. **Link expiry** (`api`, `redirector`) — add optional `expires_at` on create;
   `redirector` returns 410 Gone for expired links.
   _Acceptance: POST /links accepts `expires_at`; GET /{code} on an expired
   link returns 410, not a redirect._

2. **Custom alias validation** (`api`) — restrict user-supplied `code` to
   `[a-zA-Z0-9_-]{3,32}`, reject reserved words (`healthz`, `links`).
   _Acceptance: POST /links with `code: "healthz"` or `code: "a"` returns 400._

3. **Delete endpoint** (`api`) — `DELETE /links/{code}` removes a link (and
   its clicks via cascade).
   _Acceptance: DELETE then GET /links no longer lists the code; redirector
   404s on it._

4. **Pagination on GET /links** (`api`) — `?limit=&offset=` (or cursor-based),
   default limit 50.
   _Acceptance: creating 60 links and calling GET /links?limit=10 returns
   exactly 10, newest first._

5. **Rate-limit the redirector** (`redirector`) — simple in-memory token
   bucket per IP, 429 on exceed.
   _Acceptance: N+1th request within the window from the same IP gets 429;
   request from a different IP still succeeds._

6. **Click-count endpoint** (`api`) — `GET /links/{code}/stats` returning
   `{code, click_count, created_at}`.
   _Acceptance: after a few redirects + one worker cycle, the endpoint
   reflects the updated count; 404 for unknown code._

7. **QR-code endpoint** (`api`) — `GET /links/{code}/qr` returns a PNG QR
   code encoding the short URL.
   _Acceptance: response Content-Type is image/png and decodes back to the
   short URL._

8. **/metrics endpoint** (`api`, `redirector`) — expose Prometheus-format
   counters (requests total, redirects total, errors total).
   _Acceptance: GET /metrics returns text/plain in Prometheus exposition
   format with at least one counter that increments across requests._

9. **Structured logging** (`web`) — swap `log.Printf` for `log/slog` with
   JSON output, request method/path/status/duration per request.
   _Acceptance: a request to any web route emits one JSON log line with
   those fields._

10. **Worker retry with backoff** (`worker`) — on `ProcessClicks` error, retry
    with exponential backoff instead of just logging and continuing.
    _Acceptance: a unit test with an injectable failing store shows 3
    attempts with increasing delay before giving up on one cycle._

11. **Web search box** (`web`) — add a form on the index page to filter the
    listed links by code or target URL substring.
    _Acceptance: submitting a query re-renders the table filtered
    server-side; empty query shows all links._

12. **API auth token** (`api`) — require `Authorization: Bearer <token>` on
    `POST /links` (env-configured static token to start); `GET /links` stays
    open.
    _Acceptance: POST without/with wrong token returns 401; correct token
    succeeds; GET /links unaffected._

13. **More store tests** (`internal/store`) — cover concurrent `CreateLink`
    code collisions, `ProcessClicks` under concurrent `RecordClick` writes,
    and `ListLinks` ordering with equal timestamps.
    _Acceptance: new table-driven tests pass against the real Postgres test
    harness (`newTestStore`), no changes to production code required._

14. **Dockerize migrations** (`worker` or new `cmd/migrate`) — a tiny
    `cmd/migrate` binary that applies `migrations/*.sql` idempotently, so
    deploys don't depend on a human running `psql` by hand.
    _Acceptance: running the new binary twice against a fresh DB is a no-op
    the second time and `docker-compose up` no longer needs the manual
    `psql -f migrations/...` step documented anywhere._
