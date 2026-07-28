package worker

import (
	"context"
	"log/slog"

	"github.com/DharmilG/go-commerce/internal/order"
)

// LogNotifier is the development stub for email and push providers.
type LogNotifier struct {
	logger *slog.Logger
}

func NewLogNotifier(logger *slog.Logger) *LogNotifier {
	return &LogNotifier{logger: logger}
}

func (n *LogNotifier) NotifyOrderCreated(ctx context.Context, orderData order.Order) error {
	n.logger.InfoContext(ctx, "order confirmation stub",
		"order_id", orderData.ID,
		"user_id", orderData.UserID,
		"total_cents", orderData.TotalCents,
	)
	n.logger.InfoContext(ctx, "order notification stub",
		"order_id", orderData.ID,
		"items", len(orderData.Items),
	)
	return nil
}
