package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReceiptSenderBusinessDecision(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		wantCalls int
		wantErr   error
	}{
		{name: "paid order hands assets to buyer", status: "paid", wantCalls: 1},
		{name: "pending order sends nothing", status: "pending", wantErr: ErrOrderNotPaid},
		{name: "cancelled order sends nothing", status: "cancelled", wantErr: ErrOrderNotPaid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost {
					t.Fatalf("method = %s", r.Method)
				}
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Fatal("missing bearer authorization")
				}
				if r.Header.Get("Idempotency-Key") != "marketplace-receipt-ord_42" {
					t.Fatal("missing stable idempotency key")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if len(body) != 3 || body["to"] != "buyer@example.com" {
					t.Fatalf("email body = %#v", body)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true,"data":{"message_id":"msg_42"},"error":null,"metadata":{}}`))
			}))
			defer server.Close()

			sender := NewReceiptSender("test-key", server.Client())
			sender.endpoint = server.URL
			handoff, err := sender.Send(context.Background(), sampleOrder(tt.status))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if calls != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", calls, tt.wantCalls)
			}
			if tt.wantErr == nil && (handoff.State != "receipt_sent" || handoff.MessageID != "msg_42") {
				t.Fatalf("handoff = %#v", handoff)
			}
		})
	}
}

func sampleOrder(status string) Order {
	return Order{
		ID:           "ord_42",
		Status:       status,
		BuyerEmail:   "buyer@example.com",
		BuyerName:    "Riley",
		SellerName:   "Northwind Prints",
		TotalCents:   2400,
		Currency:     "usd",
		SellerAssets: []SellerAsset{{Name: "Print pack", DownloadURL: "https://example.com/download/ord_42"}},
	}
}
