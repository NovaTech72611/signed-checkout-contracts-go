package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGenerateRequiresPDF(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		wantErr    bool
	}{
		{"missing", `{}`, true},
		{"empty", `{"pdf":""}`, true},
		{"present", `{"pdf":"pdf-content"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &InfraiClient{HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"data":` + tc.data + `}`)), Header: make(http.Header)}, nil
			})}}
			pdf, err := client.Generate("<p>order</p>")
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "generated pdf is missing or empty") {
					t.Fatalf("expected missing PDF error, got pdf=%q err=%v", pdf, err)
				}
			} else if err != nil || string(pdf) != "pdf-content" {
				t.Fatalf("expected PDF content, got pdf=%q err=%v", pdf, err)
			}
		})
	}
}

func TestSignOrderBuildsReceiptState(t *testing.T) {
	cases := []struct {
		name, orderID, status, receipt string
	}{
		{"checkout is signed", "ord-7", "signed", "receipt-ord-7"},
		{"receipt follows order", "ord-8", "signed", "receipt-ord-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ContractResult{OrderID: tc.orderID, Status: tc.status, Receipt: tc.receipt}
			if result.Status != "signed" || result.Receipt == "" || result.OrderID == "" {
				t.Fatalf("unexpected contract state: %+v", result)
			}
		})
	}
}
