package exchange

import "time"

type APIResponse struct {
	Result             string             `json:"result"`
	BaseCode           string             `json:"base_code"`
	ConversionRates    map[string]float64 `json:"conversion_rates"`
	TimeLastUpdateUnix int64              `json:"time_last_update_unix"`
	TimeNextUpdateUnix int64              `json:"time_next_update_unix"`
}

type Rate struct {
	CurrencyCode string    `gorm:"primaryKey;size:3"`
	RateToRSD    float64   `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`
	ValidUntil   time.Time `gorm:"not null"`
}

func (Rate) TableName() string {
	return "exchange_rates"
}
