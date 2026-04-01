package main

import (
	"fmt"
	"strings"
)

type address struct {
	City    string
	Country string
	ZIP     int
}

type contact struct {
	Email  string
	Phones []string
}

type customer struct {
	ID       int
	Name     string
	Active   bool
	Address  address
	Contact  *contact
	Tags     []string
	Metadata map[string]string
}

type invoiceLine struct {
	SKU       string
	Qty       int
	UnitPrice int64
}

type invoice struct {
	Number   string
	Customer customer
	Lines    []invoiceLine
	Notes    map[string]string
}

type searchResult struct {
	Invoice   *invoice
	Matches   []string
	Preview   []byte
	Retryable bool
}

func inspectLocals(query string) string {
	normalized := strings.ToLower(strings.TrimSpace(query))
	primaryContact := &contact{
		Email:  "ops@acme.test",
		Phones: []string{"+34-555-0101", "+52-555-0199"},
	}
	acct := customer{
		ID:      42,
		Name:    "Acme Latam",
		Active:  true,
		Address: address{City: "Barcelona", Country: "ES", ZIP: 8039},
		Contact: primaryContact,
		Tags:    []string{"priority", "export", "emea"},
		Metadata: map[string]string{
			"owner":  "sales",
			"region": "latam",
		},
	}
	currentInvoice := &invoice{
		Number:   "INV-2026-0007",
		Customer: acct,
		Lines: []invoiceLine{
			{SKU: "db-page", Qty: 3, UnitPrice: 1999},
			{SKU: "index-scan", Qty: 1, UnitPrice: 4999},
		},
		Notes: map[string]string{
			"currency": "EUR",
			"status":   "open",
		},
	}
	result := searchResult{
		Invoice:   currentInvoice,
		Matches:   []string{"acme", "latam", normalized},
		Preview:   []byte("rowid=42|country=sv|name=el salvador"),
		Retryable: false,
	}
	aliases := []string{"acme", "acme corp", "acme latam"}
	stats := map[string]int{"rows": 3, "hits": 2, "pages": 14}
	lookupErr := fmt.Errorf("simulated warning for %q", normalized)

	summary := fmt.Sprintf("%s/%d/%d", acct.Name, len(result.Matches), stats["hits"])
	if lookupErr != nil {
		summary += "/warn"
	}
	if len(aliases) > 0 && result.Invoice != nil {
		summary += "/" + result.Invoice.Number
	}
	return summary
}

func main() {
	summary := inspectLocals("  ACME LATAM  ")
	fmt.Println(summary)
}
