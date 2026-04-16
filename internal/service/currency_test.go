package service

import (
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetCurrencyInfo_KnownCurrencies(t *testing.T) {
	tests := []struct {
		code           string
		expectedSymbol string
		expectedName   string
	}{
		{"USD", "$", "US Dollar"},
		{"EUR", "€", "Euro"},
		{"GBP", "£", "British Pound"},
		{"JPY", "¥", "Japanese Yen"},
		{"INR", "₹", "Indian Rupee"},
		{"BRL", "R$", "Brazilian Real"},
		{"COP", "COL$", "Colombian Peso"},
		{"BDT", "৳", "Bangladeshi Taka"},
		{"AED", "د.إ", "UAE Dirham"},
		{"CZK", "Kč", "Czech Koruna"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			info := GetCurrencyInfo(tt.code)
			assert.Equal(t, tt.code, info.Code)
			assert.Equal(t, tt.expectedSymbol, info.Symbol)
			assert.Equal(t, tt.expectedName, info.Name)
		})
	}
}

func TestGetCurrencyInfo_UnknownCurrency(t *testing.T) {
	info := GetCurrencyInfo("XYZ")
	assert.Equal(t, "XYZ", info.Code)
	assert.Equal(t, "XYZ", info.Symbol, "Unknown currency should use code as symbol")
	assert.Equal(t, "XYZ", info.Name, "Unknown currency should use code as name")
}

func TestGetCurrencyInfo_EmptyCode(t *testing.T) {
	info := GetCurrencyInfo("")
	assert.Equal(t, "", info.Code)
	assert.Equal(t, "", info.Symbol)
	assert.Equal(t, "", info.Name)
}

func TestGetAvailableCurrencies(t *testing.T) {
	currencies := GetAvailableCurrencies()

	assert.Equal(t, len(BuiltinCurrencies), len(currencies))
	assert.True(t, len(currencies) >= 35, "Should have at least 35 currencies")

	// Verify first and last entries match
	assert.Equal(t, "USD", currencies[0].Code)
	assert.Equal(t, "RON", currencies[len(currencies)-1].Code)
}

func TestSupportedCurrencies_DerivedFromBuiltin(t *testing.T) {
	assert.Equal(t, len(BuiltinCurrencies), len(SupportedCurrencies))

	for i, info := range BuiltinCurrencies {
		assert.Equal(t, info.Code, SupportedCurrencies[i], "SupportedCurrencies should match BuiltinCurrencies order")
	}
}

func TestCurrencyInfoMap_AllEntriesPresent(t *testing.T) {
	for _, info := range BuiltinCurrencies {
		mapped, ok := currencyInfoMap[info.Code]
		assert.True(t, ok, "Currency %s should be in currencyInfoMap", info.Code)
		assert.Equal(t, info, mapped)
	}
}

func TestCurrencySymbolForCode(t *testing.T) {
	tests := []struct {
		code     string
		expected string
	}{
		{"USD", "$"},
		{"EUR", "€"},
		{"GBP", "£"},
		{"COP", "COL$"},
		{"UNKNOWN", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			assert.Equal(t, tt.expected, CurrencySymbolForCode(tt.code))
		})
	}
}

func TestCurrencySymbolForSubscription(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	err = db.AutoMigrate(&models.Settings{})
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	settingsService.SetCurrency("USD")

	tests := []struct {
		name             string
		originalCurrency string
		expectedSymbol   string
	}{
		{
			name:             "Same as preferred currency",
			originalCurrency: "USD",
			expectedSymbol:   "$",
		},
		{
			name:             "Different from preferred currency",
			originalCurrency: "EUR",
			expectedSymbol:   "€",
		},
		{
			name:             "Empty original currency uses preferred",
			originalCurrency: "",
			expectedSymbol:   "$",
		},
		{
			name:             "COP shows COL$ symbol",
			originalCurrency: "COP",
			expectedSymbol:   "COL$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := &models.Subscription{
				Name:             "Test",
				OriginalCurrency: tt.originalCurrency,
			}
			symbol := currencySymbolForSubscription(sub, settingsService)
			assert.Equal(t, tt.expectedSymbol, symbol)
		})
	}
}
