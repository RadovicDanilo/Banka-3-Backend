package trading

import "testing"

func TestMaintenanceMargin(t *testing.T) {
	cases := []struct {
		name         string
		contractSize int64
		basePrice    int64
		quantity     int64
		permille     int64
		want         int64
	}{
		// Stock: 50% × 10000 × 2 = 10000
		{"stock 2 shares @100", 1, 10000, 2, 500, 10000},
		// Future: contract_size 100 × 26500000 × 1 × 10% = 265000000
		{"future gold 1 contract", 100, 26500000, 1, 100, 265000000},
		// Forex: 1000 × 11715 × 1 × 10% = 1171500
		{"forex EUR/RSD 1 lot", 1000, 11715, 1, 100, 1171500},
		// Option: 1 × stock_price 20000 × 3 × 50% = 30000
		{"option 3 contracts", 1, 20000, 3, 500, 30000},
		// Zero quantity → zero
		{"zero qty", 1, 10000, 0, 500, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := maintenanceMargin(c.contractSize, c.basePrice, c.quantity, c.permille); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestInitialMarginCost(t *testing.T) {
	cases := []struct {
		name string
		mm   int64
		want int64
	}{
		// IMC = MM × 1.1
		{"round", 10000, 11000},
		{"odd → truncates", 10001, 11001}, // 10001*11/10 = 11001 (integer div)
		{"zero", 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := initialMarginCost(c.mm); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}
