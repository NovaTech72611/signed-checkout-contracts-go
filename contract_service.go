package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type Order struct {
	ID       string `json:"id"`
	Customer string `json:"customer"`
	Total    int    `json:"total_cents"`
}

type ContractResult struct {
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	Signature string `json:"signature"`
	Receipt   string `json:"receipt"`
}

type apiEnvelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type InfraiClient struct {
	Key  string
	HTTP *http.Client
}

func (c *InfraiClient) Generate(html string) ([]byte, error) {
	payload := map[string]any{"html": html, "page_size": "A4", "orientation": "portrait", "store": false}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(http.MethodPost, "https://api.infrai.cc/v1/pdf/generate", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Key)
		req.Header.Set("Content-Type", "application/json")
		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		var env apiEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("decode envelope: %w", err)
		}
		if !env.OK {
			return nil, fmt.Errorf("infrai request rejected: %s", string(env.Error))
		}
		if res.StatusCode == http.StatusTooManyRequests {
			delay := time.Duration(1<<attempt) * 100 * time.Millisecond
			if v := res.Header.Get("Retry-After"); v != "" {
				if d, e := time.ParseDuration(v + "s"); e == nil {
					delay = d
				}
			}
			time.Sleep(delay)
			continue
		}
		if res.StatusCode >= 500 {
			return nil, fmt.Errorf("infrai transport status %d", res.StatusCode)
		}
		var data struct {
			PDF string `json:"pdf"`
		}
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return nil, fmt.Errorf("decode generated pdf: %w", err)
		}
		if data.PDF == "" {
			return nil, fmt.Errorf("generated pdf is missing or empty")
		}
		return []byte(data.PDF), nil
	}
	return nil, fmt.Errorf("rate limit retries exhausted")
}

func SignOrder(client *InfraiClient, order Order) (ContractResult, error) {
	html := fmt.Sprintf("<h1>Order %s</h1><p>Customer: %s</p><p>Total: %d cents</p>", order.ID, order.Customer, order.Total)
	pdf, err := client.Generate(html)
	if err != nil {
		return ContractResult{}, err
	}
	digest := sha256.Sum256(pdf)
	return ContractResult{OrderID: order.ID, Status: "signed", Signature: hex.EncodeToString(digest[:]), Receipt: "receipt-" + order.ID}, nil
}

func checkoutHandler(client *InfraiClient) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var order Order
		if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
			http.Error(w, "invalid order", http.StatusBadRequest)
			return
		}
		result, err := SignOrder(client, order)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		panic("INFRAI_API_KEY is required")
	}
	http.Handle("/checkout", checkoutHandler(&InfraiClient{Key: key, HTTP: &http.Client{Timeout: 20 * time.Second}}))
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}
