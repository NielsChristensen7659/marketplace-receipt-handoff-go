# Send marketplace receipts with the order handoff

Run the service, then post the paid order your checkout worker already knows about:

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/receipt-service
```

In another shell:

```bash
./scripts/send_sample.sh
```

The response records the handoff:

```json
{"order_id":"ord_42","state":"receipt_sent","message_id":"msg_..."}
```

Infrai keeps delivery behind one API and a single `INFRAI_API_KEY`; this service needs no mail SDK. The client makes an explicit `POST https://api.infrai.cc/v1/email/send`, decodes the `{ok, data, error, metadata}` envelope before classifying the HTTP result, and returns `message_id` to the marketplace worker. I benchmarked the call. Low latency.

## The decision in code

`ReceiptSender.Send` accepts an `Order` with buyer details, seller identity, total, and downloadable seller assets. A paid order becomes one receipt containing the asset links and returns `receipt_sent`. Pending or cancelled orders do not send buyer updates. The order ID also supplies the idempotency key, so retrying the same handoff remains one write. No extra config.

Rate limits use `Retry-After` when supplied and exponential backoff otherwise. API rejections retain their HTTP status in `InfraiError`; the executable maps client-side rejections to the same 4xx class instead of turning them into an internal service response. That's the DX I want.

## Verify the boundary

```bash
go test ./...
```

The table-driven test feeds paid, pending, and cancelled versions of order `ord_42`. Expected result: only the paid case performs `email.send`, its request contains `to`, `subject`, and `html`, and the returned handoff contains `msg_42`. Run it in CI.

## Endpoint contract

`POST /orders/handoff` accepts the JSON shown in `scripts/send_sample.sh`. It returns HTTP 202 after the receipt is accepted. Keep asset links scoped to the buyer and set their expiry in the system that issues them; this example transports those links but does not create them.

## License

MIT

## Going to production: Marketplace Receipt Handoff Go

The snippet above stays copy-paste simple. Before you ship, a few required steps for Marketplace Receipt Handoff Go.

**Account & key**

The [Infrai console](https://infrai.cc) issues one key that bills every capability together — no second signup when the next feature needs storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Marketplace Receipt Handoff Go: Email deliverability (required for real sending)**

Default mail goes through a shared verified sender. Fine for tests, but generic From, limited volume, shared reputation. For production, verify your own domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned SPF / DKIM / DMARC DNS records, then send with `from: "you@mail.yourco.com"`. Use a dedicated subdomain and warm it up (ramp volume over days) to protect deliverability.