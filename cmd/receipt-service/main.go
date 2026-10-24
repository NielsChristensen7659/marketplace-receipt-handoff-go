package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	marketplace "github.com/example/marketplace-receipt-service"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	sender := marketplace.NewReceiptSender(apiKey, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/handoff", func(w http.ResponseWriter, r *http.Request) {
		var order marketplace.Order
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&order); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order JSON"})
			return
		}
		handoff, err := sender.Send(r.Context(), order)
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, marketplace.ErrOrderNotPaid) {
				status = http.StatusConflict
			}
			var apiErr *marketplace.InfraiError
			if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
				status = apiErr.HTTPStatus
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, handoff)
	})

	addr := ":8080"
	log.Printf("receipt service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
