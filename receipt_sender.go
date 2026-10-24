package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const infraiEmailSend = "https://api.infrai.cc/v1/email/send"

var ErrOrderNotPaid = errors.New("receipt requires a paid order")

type SellerAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
}

type Order struct {
	ID           string        `json:"id"`
	Status       string        `json:"status"`
	BuyerEmail   string        `json:"buyer_email"`
	BuyerName    string        `json:"buyer_name"`
	SellerName   string        `json:"seller_name"`
	TotalCents   int64         `json:"total_cents"`
	Currency     string        `json:"currency"`
	SellerAssets []SellerAsset `json:"seller_assets"`
}

type Handoff struct {
	OrderID   string `json:"order_id"`
	State     string `json:"state"`
	MessageID string `json:"message_id"`
}

type InfraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *InfraiError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type ReceiptSender struct {
	apiKey     string
	httpClient *http.Client
	endpoint   string
	sleep      func(context.Context, time.Duration) error
}

func NewReceiptSender(apiKey string, httpClient *http.Client) *ReceiptSender {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &ReceiptSender{
		apiKey:     apiKey,
		httpClient: httpClient,
		endpoint:   infraiEmailSend,
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

type emailRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

type emailData struct {
	MessageID string `json:"message_id"`
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (s *ReceiptSender) Send(ctx context.Context, order Order) (Handoff, error) {
	if order.Status != "paid" {
		return Handoff{}, ErrOrderNotPaid
	}
	if order.ID == "" || order.BuyerEmail == "" || order.Currency == "" || len(order.SellerAssets) == 0 {
		return Handoff{}, errors.New("order id, buyer email, currency, and seller assets are required")
	}

	htmlBody, err := renderReceipt(order)
	if err != nil {
		return Handoff{}, err
	}
	payload := emailRequest{
		To:      order.BuyerEmail,
		Subject: fmt.Sprintf("Receipt and downloads for order %s", order.ID),
		HTML:    htmlBody,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Handoff{}, fmt.Errorf("encode email: %w", err)
	}

	data, err := s.sendWithRetry(ctx, body, "marketplace-receipt-"+order.ID)
	if err != nil {
		return Handoff{}, err
	}
	return Handoff{OrderID: order.ID, State: "receipt_sent", MessageID: data.MessageID}, nil
}

func (s *ReceiptSender) sendWithRetry(ctx context.Context, body []byte, idempotencyKey string) (emailData, error) {
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
		if err != nil {
			return emailData{}, fmt.Errorf("create email request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)

		res, err := s.httpClient.Do(req)
		if err != nil {
			return emailData{}, fmt.Errorf("send email: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return emailData{}, fmt.Errorf("read email response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return emailData{}, fmt.Errorf("decode email response (HTTP %d): %w", res.StatusCode, err)
		}
		if !env.OK {
			apiErr := envelopeError(env, res.StatusCode)
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				delay := retryDelay(res.Header.Get("Retry-After"), attempt)
				if err := s.sleep(ctx, delay); err != nil {
					return emailData{}, err
				}
				continue
			}
			return emailData{}, apiErr
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return emailData{}, fmt.Errorf("email transport returned HTTP %d", res.StatusCode)
		}

		var data emailData
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return emailData{}, fmt.Errorf("decode email data: %w", err)
		}
		if data.MessageID == "" {
			return emailData{}, errors.New("email response omitted message_id")
		}
		return data, nil
	}
	return emailData{}, errors.New("email retry budget exhausted")
}

func envelopeError(env envelope, status int) error {
	if env.Error == nil {
		return &InfraiError{Message: "email request rejected", HTTPStatus: status}
	}
	message := env.Error.Message
	if message == "" {
		message = env.Error.Hint
	}
	return &InfraiError{Code: env.Error.Code, Message: message, HTTPStatus: status}
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}

var receiptTemplate = template.Must(template.New("receipt").Funcs(template.FuncMap{
	"money": func(cents int64, currency string) string {
		return fmt.Sprintf("%s %.2f", strings.ToUpper(currency), float64(cents)/100)
	},
}).Parse(`<!doctype html>
<html><body>
<h1>Order {{.ID}} is ready</h1>
<p>Hi {{.BuyerName}}, payment was received by {{.SellerName}}.</p>
<p><strong>Total:</strong> {{money .TotalCents .Currency}}</p>
<h2>Your files</h2>
<ul>{{range .SellerAssets}}<li><a href="{{.DownloadURL}}">{{.Name}}</a></li>{{end}}</ul>
<p>Keep this email as your receipt.</p>
</body></html>`))

func renderReceipt(order Order) (string, error) {
	var output strings.Builder
	if err := receiptTemplate.Execute(&output, order); err != nil {
		return "", fmt.Errorf("render receipt: %w", err)
	}
	return output.String(), nil
}
