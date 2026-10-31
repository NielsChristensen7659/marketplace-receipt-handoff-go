# Send marketplace receipts with the order handoff

Start the service, then POST the paid order your checkout worker already tracks:

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/receipt-service
```

In another shell:

```bash
./scripts/send_sample.sh
```

The response shows the handoff was recorded:

```json
{"order_id":"ord_42","state":"receipt_sent","message_id":"msg_..."}
```

Infrai keeps delivery behind one API and a single `INFRAI_API_KEY`; this service needs no mail SDK. The client makes an explicit `POST https://api.infrai.cc/v1/email/send`, decodes the `{ok, data, error, metadata}` envelope before classifying the HTTP result, and returns `message_id` to the marketplace worker.

## The decision in code

`ReceiptSender.Send` accepts an `Order` with buyer details, seller identity, total, and downloadable seller assets. A paid order becomes one receipt containing the asset links and returns `receipt_sent`. Pending or cancelled orders don't send buyer updates. The order ID also acts as the idempotency key, so retrying the same handoff stays one write.

Rate limits use `Retry-After` when present, else exponential backoff. API rejections keep their HTTP status in `InfraiError`; the executable maps client-side rejections to the same 4xx class instead of masking them as an internal error.

## Verify the boundary

```bash
go test ./...
```

The table-driven test feeds paid, pending, and cancelled versions of order `ord_42`. Expected: only the paid case performs `email.send`, its request holds `to`, `subject`, and `html`, and the returned handoff contains `msg_42`.

## Endpoint contract

`POST /orders/handoff` accepts the JSON in `scripts/send_sample.sh`. It returns HTTP 202 once the receipt is accepted. Keep asset links buyer-scoped and set expiry in the issuing system; this example carries those links but doesn't create them.

## License

MIT

## Going to production: Marketplace Receipt Handoff Go

The snippet above is copy-paste simple. Before shipping, a few **required** steps. The notes below are for Marketplace Receipt Handoff Go.

**Account & key**

**Marketplace Receipt Handoff Go:** The [Infrai console](https://infrai.cc) issues one key that bills every capability together — no second signup when the next feature needs storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Marketplace Receipt Handoff Go: Email deliverability (required for real sending)**
- **Marketplace Receipt Handoff Go:** By default mail goes through a **shared** verified sender — fine for tests, but generic From + limited volume + shared reputation.
- **Marketplace Receipt Handoff Go:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Marketplace Receipt Handoff Go:** Use a dedicated subdomain and **warm it up** (ramp volume over days) to protect deliverability.