package bank

import (
	"strconv"
	"strings"
	"testing"

	"github.com/theplant/luhn"
)

func TestGenerateCardNumber(t *testing.T) {
	tests := []struct {
		cardBrand  card_brand
		accountNum string
		wantLen    int
		prefix     string
	}{
		{visa, "12345678901", 16, "4"},
		{mastercard, "9876543210", 16, "51"},
		{amex, "1122334455", 15, "34"},
		{dinacard, "0000000000", 16, "9891"},
	}

	for _, tt := range tests {
		t.Run(string(tt.cardBrand), func(t *testing.T) {
			got, err := GenerateCardNumber(tt.cardBrand, tt.accountNum)
			if err != nil {
				t.Fatalf("GenerateCardNumber failed: %v", err)
			}

			if len(got) != tt.wantLen {
				t.Errorf("Got length %d, want %d", len(got), tt.wantLen)
			}

			if !strings.HasPrefix(got, tt.prefix) {
				t.Errorf("Card %s missing prefix %s", got, tt.prefix)
			}

			val, _ := strconv.ParseInt(got, 10, 64)
			if !luhn.Valid(int(val)) {
				t.Errorf("Card %s failed Luhn check", got)
			}
		})
	}
}

func TestGenerateCVV(t *testing.T) {
	for i := 0; i < 10; i++ {
		cvv := GenerateCVV()
		if len(cvv) != 3 {
			t.Errorf("Got CVV length %d, want 3", len(cvv))
		}
		if _, err := strconv.Atoi(cvv); err != nil {
			t.Errorf("CVV %s is not numeric", cvv)
		}
	}
}
