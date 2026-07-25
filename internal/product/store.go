package product

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ListFilter struct {
	Search   string
	Category string
	Limit    int
	Offset   int
}

type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

func (s *Store) List(ctx context.Context, filter ListFilter) ([]Product, int, error) {
	where := []string{"is_active = true"}
	args := []any{}

	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR sku ILIKE $%d)", len(args), len(args)))
	}
	if filter.Category != "" {
		args = append(args, filter.Category)
		where = append(where, fmt.Sprintf("category = $%d", len(args)))
	}

	whereSQL := " WHERE " + strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRow(ctx, "SELECT count(*) FROM products"+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	args = append(args, filter.Limit, filter.Offset)
	query := fmt.Sprintf(
		`SELECT id, sku, name, description, price_cents, stock, category, image_url, is_active, created_at, updated_at
		 FROM products%s
		 ORDER BY created_at DESC, id DESC
		 LIMIT $%d OFFSET $%d`,
		whereSQL, len(args)-1, len(args),
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	products := make([]Product, 0, filter.Limit)
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate products: %w", err)
	}
	return products, total, nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID, includeInactive bool) (Product, error) {
	query := `SELECT id, sku, name, description, price_cents, stock, category, image_url, is_active, created_at, updated_at
	          FROM products WHERE id = $1`
	if !includeInactive {
		query += " AND is_active = true"
	}

	product, err := scanProduct(s.db.QueryRow(ctx, query, id.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	return product, err
}

func (s *Store) GetMany(ctx context.Context, ids []uuid.UUID) ([]Product, error) {
	if len(ids) == 0 {
		return []Product{}, nil
	}

	args := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id.String()
	}

	rows, err := s.db.Query(ctx,
		`SELECT id, sku, name, description, price_cents, stock, category, image_url, is_active, created_at, updated_at
		 FROM products WHERE id = ANY($1::uuid[])`,
		args,
	)
	if err != nil {
		return nil, fmt.Errorf("get products: %w", err)
	}
	defer rows.Close()

	products := make([]Product, 0, len(ids))
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate products: %w", err)
	}
	return products, nil
}

func (s *Store) Create(ctx context.Context, input CreateInput) (Product, error) {
	id := uuid.New()
	active := true
	if input.IsActive != nil {
		active = *input.IsActive
	}

	product, err := scanProduct(s.db.QueryRow(ctx,
		`INSERT INTO products (id, sku, name, description, price_cents, stock, category, image_url, is_active)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, sku, name, description, price_cents, stock, category, image_url, is_active, created_at, updated_at`,
		id.String(), input.SKU, input.Name, input.Description, input.PriceCents, input.Stock,
		input.Category, input.ImageURL, active,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Product{}, ErrSKUExists
		}
		return Product{}, fmt.Errorf("insert product: %w", err)
	}
	return product, nil
}

func (s *Store) Update(ctx context.Context, id uuid.UUID, input UpdateInput) (Product, error) {
	product, err := scanProduct(s.db.QueryRow(ctx,
		`UPDATE products SET
		     sku = COALESCE($2, sku),
		     name = COALESCE($3, name),
		     description = COALESCE($4, description),
		     price_cents = COALESCE($5, price_cents),
		     stock = COALESCE($6, stock),
		     category = COALESCE($7, category),
		     image_url = COALESCE($8, image_url),
		     is_active = COALESCE($9, is_active),
		     updated_at = now()
		 WHERE id = $1
		 RETURNING id, sku, name, description, price_cents, stock, category, image_url, is_active, created_at, updated_at`,
		id.String(), input.SKU, input.Name, input.Description, input.PriceCents, input.Stock,
		input.Category, input.ImageURL, input.IsActive,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Product{}, ErrSKUExists
		}
		return Product{}, fmt.Errorf("update product: %w", err)
	}
	return product, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProduct(row rowScanner) (Product, error) {
	var (
		product Product
		id      string
	)
	err := row.Scan(
		&id, &product.SKU, &product.Name, &product.Description, &product.PriceCents,
		&product.Stock, &product.Category, &product.ImageURL, &product.IsActive,
		&product.CreatedAt, &product.UpdatedAt,
	)
	if err != nil {
		return Product{}, err
	}
	product.ID, err = uuid.Parse(id)
	if err != nil {
		return Product{}, fmt.Errorf("parse product id: %w", err)
	}
	return product, nil
}
