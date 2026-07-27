package order

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestValidateIdempotencyKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr error
	}{
		{name: "minimum length", key: "abcdefgh"},
		{name: "maximum length", key: strings.Repeat("a", 255)},
		{name: "uuid allowed", key: uuid.NewString()},
		{name: "empty", key: "", wantErr: ErrIdempotencyKeyRequired},
		{name: "too short", key: "abcdefg", wantErr: ErrInvalidIdempotencyKey},
		{name: "too long", key: strings.Repeat("a", 256), wantErr: ErrInvalidIdempotencyKey},
		{name: "space", key: "abc defgh", wantErr: ErrInvalidIdempotencyKey},
		{name: "newline", key: "abcdefg\n", wantErr: ErrInvalidIdempotencyKey},
		{name: "non-ascii", key: "abcdefgé", wantErr: ErrInvalidIdempotencyKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIdempotencyKey(tt.key)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateIdempotencyKey() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestAddressValidation(t *testing.T) {
	valid := Address{
		FullName:   "Test Learner",
		Line1:      "1 Test Street",
		City:       "Ahmedabad",
		State:      "Gujarat",
		PostalCode: "380001",
		Country:    "IN",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid address rejected: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*Address)
		wantErr bool
	}{
		{name: "missing full name", mutate: func(a *Address) { a.FullName = "" }, wantErr: true},
		{name: "missing line1", mutate: func(a *Address) { a.Line1 = "" }, wantErr: true},
		{name: "missing city", mutate: func(a *Address) { a.City = "" }, wantErr: true},
		{name: "missing postal code", mutate: func(a *Address) { a.PostalCode = "" }, wantErr: true},
		{name: "missing country", mutate: func(a *Address) { a.Country = "" }, wantErr: true},
		{name: "short country", mutate: func(a *Address) { a.Country = "I" }, wantErr: true},
		{name: "long city", mutate: func(a *Address) { a.City = strings.Repeat("a", 101) }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			address := valid
			tt.mutate(&address)
			err := address.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAddressNormalizeTrimsEveryField(t *testing.T) {
	address := Address{
		FullName:   "  Test Learner  ",
		Line1:      " 1 Test Street ",
		Line2:      " Unit 2 ",
		City:       " Ahmedabad ",
		State:      " Gujarat ",
		PostalCode: " 380001 ",
		Country:    " IN ",
	}
	address.Normalize()
	if address.FullName != "Test Learner" || address.Line1 != "1 Test Street" ||
		address.Line2 != "Unit 2" || address.City != "Ahmedabad" ||
		address.State != "Gujarat" || address.PostalCode != "380001" || address.Country != "IN" {
		t.Fatalf("Normalize() left whitespace: %+v", address)
	}
}

func TestRequestHashIsDeterministicAndNormalized(t *testing.T) {
	first := CreateRequest{ShippingAddress: Address{
		FullName: " Test Learner ", Line1: "1 Test Street", City: "Ahmedabad",
		PostalCode: "380001", Country: "IN",
	}}
	second := CreateRequest{ShippingAddress: Address{
		FullName: "Test Learner", Line1: "1 Test Street", City: "Ahmedabad",
		PostalCode: "380001", Country: "IN",
	}}
	first.ShippingAddress.Normalize()
	second.ShippingAddress.Normalize()

	firstHash, err := hashRequest(first)
	if err != nil {
		t.Fatalf("hashRequest() error = %v", err)
	}
	secondHash, err := hashRequest(second)
	if err != nil {
		t.Fatalf("hashRequest() error = %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("normalized requests hashed differently: %s != %s", firstHash, secondHash)
	}

	second.ShippingAddress.Line1 = "2 Different Street"
	differentHash, err := hashRequest(second)
	if err != nil {
		t.Fatalf("hashRequest() error = %v", err)
	}
	if firstHash == differentHash {
		t.Fatal("different addresses produced the same hash")
	}
}

func TestStockErrorUnwrapsToSentinel(t *testing.T) {
	err := &StockError{ProductID: uuid.New(), Requested: 2, Available: 1}
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatal("StockError does not unwrap to ErrInsufficientStock")
	}
}

func TestProductUnavailableErrorUnwrapsToSentinel(t *testing.T) {
	err := &ProductUnavailableError{ProductID: uuid.New()}
	if !errors.Is(err, ErrProductUnavailable) {
		t.Fatal("ProductUnavailableError does not unwrap to ErrProductUnavailable")
	}
}
