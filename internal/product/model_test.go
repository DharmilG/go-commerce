package product

import (
	"strings"
	"testing"
)

func TestCreateInputValidation(t *testing.T) {
	valid := CreateInput{
		SKU:        "SKU-1",
		Name:       "Test Product",
		PriceCents: 1000,
		Stock:      5,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*CreateInput)
		wantErr bool
	}{
		{name: "missing sku", mutate: func(in *CreateInput) { in.SKU = "" }, wantErr: true},
		{name: "short name", mutate: func(in *CreateInput) { in.Name = "x" }, wantErr: true},
		{name: "negative price", mutate: func(in *CreateInput) { in.PriceCents = -1 }, wantErr: true},
		{name: "negative stock", mutate: func(in *CreateInput) { in.Stock = -1 }, wantErr: true},
		{name: "long category", mutate: func(in *CreateInput) { in.Category = strings.Repeat("a", 101) }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := valid
			tt.mutate(&input)
			err := input.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUpdateInputValidation(t *testing.T) {
	if err := (&UpdateInput{}).Validate(); err == nil {
		t.Fatal("empty update was accepted")
	}

	negativePrice := int64(-1)
	if err := (&UpdateInput{PriceCents: &negativePrice}).Validate(); err == nil {
		t.Fatal("negative price update was accepted")
	}

	emptyName := ""
	if err := (&UpdateInput{Name: &emptyName}).Validate(); err == nil {
		t.Fatal("empty name update was accepted")
	}

	validPrice := int64(1200)
	if err := (&UpdateInput{PriceCents: &validPrice}).Validate(); err != nil {
		t.Fatalf("valid update rejected: %v", err)
	}
}

func TestCreateInputNormalize(t *testing.T) {
	input := CreateInput{
		SKU:         "  SKU-1 ",
		Name:        " Test Product ",
		Description: " Description ",
		Category:    " Category ",
		ImageURL:    " https://example.com/image.jpg ",
	}
	input.Normalize()
	if input.SKU != "SKU-1" || input.Name != "Test Product" || input.Description != "Description" ||
		input.Category != "Category" || input.ImageURL != "https://example.com/image.jpg" {
		t.Fatalf("Normalize() left whitespace: %+v", input)
	}
}
