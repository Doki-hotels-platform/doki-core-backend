package billing

import (
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/types"
)

type AccountType string

const (
	AccountGuestDeposit    AccountType = "GUEST_DEPOSIT"
	AccountHotelPayable    AccountType = "HOTEL_PAYABLE"
	AccountPlatformRevenue AccountType = "PLATFORM_REVENUE"
	AccountTaxPayable      AccountType = "TAX_PAYABLE"
)

type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry represents an immutable single entry in the double-entry accounting ledger.
type LedgerEntry struct {
	ID            uuid.UUID   `json:"id"`
	TransactionID uuid.UUID   `json:"transaction_id"`
	AccountType   AccountType `json:"account_type"`
	EntryType     EntryType   `json:"entry_type"`
	Amount        types.Money `json:"amount"`
	Description   string      `json:"description"`
	CreatedAt     time.Time   `json:"created_at"`
}
