package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DharmilG/go-commerce/internal/auth"
	"github.com/DharmilG/go-commerce/internal/config"
	"github.com/DharmilG/go-commerce/internal/logging"
	"github.com/DharmilG/go-commerce/internal/postgres"
	"github.com/DharmilG/go-commerce/migrations"
)

type demoProduct struct {
	SKU         string
	Name        string
	Description string
	PriceCents  int64
	Stock       int
	Category    string
	ImageURL    string
}

var demoProducts = []demoProduct{
	{"KB-MECH-01", "Mechanical Keyboard", "Hot-swappable 75% keyboard with tactile switches.", 12900, 12, "Peripherals", "https://images.example.com/keyboard.jpg"},
	{"MS-ERGO-01", "Ergonomic Mouse", "Vertical wireless mouse with a rechargeable battery.", 4999, 20, "Peripherals", "https://images.example.com/mouse.jpg"},
	{"MON-27-01", "27-inch QHD Monitor", "165 Hz IPS monitor with USB-C power delivery.", 34900, 8, "Displays", "https://images.example.com/monitor.jpg"},
	{"HUB-USBC-01", "USB-C Hub", "Seven-port hub with HDMI, Ethernet, and SD reader.", 7900, 25, "Accessories", "https://images.example.com/hub.jpg"},
	{"HP-ANC-01", "Noise-Cancelling Headphones", "Over-ear headphones with active noise cancellation.", 19900, 10, "Audio", "https://images.example.com/headphones.jpg"},
	{"CAM-1080-01", "1080p Webcam", "Autofocus webcam with a privacy shutter.", 5900, 18, "Accessories", "https://images.example.com/webcam.jpg"},
	{"MAT-DESK-01", "Desk Mat", "Large stitched-edge desk mat in charcoal.", 2900, 30, "Accessories", "https://images.example.com/desk-mat.jpg"},
	{"STAND-LAP-01", "Laptop Stand", "Adjustable aluminium stand for 13-17 inch laptops.", 4500, 15, "Accessories", "https://images.example.com/stand.jpg"},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg)
	slog.SetDefault(logger)

	if err := postgres.Migrate(cfg.DatabaseURL, migrations.FS); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	for _, item := range demoProducts {
		id := uuid.NewString()
		if _, err := db.Exec(ctx,
			`INSERT INTO products
			     (id, sku, name, description, price_cents, stock, category, image_url, is_active)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true)
			 ON CONFLICT (sku) DO UPDATE SET
			     name = EXCLUDED.name,
			     description = EXCLUDED.description,
			     price_cents = EXCLUDED.price_cents,
			     stock = EXCLUDED.stock,
			     category = EXCLUDED.category,
			     image_url = EXCLUDED.image_url,
			     is_active = true,
			     updated_at = now()`,
			id, item.SKU, item.Name, item.Description, item.PriceCents, item.Stock,
			item.Category, item.ImageURL,
		); err != nil {
			return fmt.Errorf("seed product %s: %w", item.SKU, err)
		}
	}
	logger.Info("seeded demo products", "count", len(demoProducts))

	adminEmail := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if adminEmail == "" && adminPassword == "" {
		logger.Info("skipping admin seed because ADMIN_EMAIL and ADMIN_PASSWORD are not set")
		return nil
	}
	if adminEmail == "" || adminPassword == "" {
		return fmt.Errorf("ADMIN_EMAIL and ADMIN_PASSWORD must be set together")
	}

	if err := auth.ValidatePassword(adminPassword); err != nil {
		return fmt.Errorf("admin password: %w", err)
	}
	hash, err := auth.HashPassword(adminPassword, cfg.BcryptCost)
	if err != nil {
		return err
	}

	tag, err := db.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, name, role)
		 VALUES ($1, $2, $3, $4, 'admin')
		 ON CONFLICT DO NOTHING`,
		uuid.NewString(), strings.ToLower(adminEmail), hash, "Store Administrator",
	)
	if err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}
	if tag.RowsAffected() == 1 {
		logger.Info("seeded admin user", "email", strings.ToLower(adminEmail))
	} else {
		logger.Info("admin user already exists; leaving the existing role unchanged", "email", strings.ToLower(adminEmail))
	}
	return nil
}
