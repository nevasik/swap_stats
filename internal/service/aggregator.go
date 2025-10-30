package service

import (
	"context"
	"dexcelerate/internal/dedupe/redis"
	"dexcelerate/internal/domain"
	"dexcelerate/internal/pubsub"
	"dexcelerate/internal/stores/clickhouse"
	"dexcelerate/internal/window"
	"errors"
	"fmt"
	"strings"

	"gitlab.com/nevasik7/alerting/logger"
)

var (
	ErrTokenNotFound = errors.New("token not found in windows")
)

// Encapsulates the logic-business for handling swap events;
// It the only point orchestration: dedup → window → broadcast → clickhouse;
// Implements from Consumer, HTTP, gRPC, CLI and etc...
type AggregatorService struct {
	log          logger.Logger
	windowEngine window.WindowEngine
	broadcaster  pubsub.Broadcaster
	chWriter     clickhouse.ClickHouseWriter
	deduper      redis.Deduplicator
}

func NewAggregatorService(
	log logger.Logger,
	windowEngine window.WindowEngine,
	broadcaster pubsub.Broadcaster,
	chWriter clickhouse.ClickHouseWriter,
	deduper redis.Deduplicator,

) *AggregatorService {
	return &AggregatorService{
		windowEngine: windowEngine,
		broadcaster:  broadcaster,
		chWriter:     chWriter,
		deduper:      deduper,
		log:          log,
	}
}

func (a *AggregatorService) ProcessSwapEvent(ctx context.Context, ev *domain.SwapEvent) error {
	isDup, err := a.deduper.IsDuplicate(ctx, ev.EventID)
	if err != nil {
		return fmt.Errorf("dedup check failed for %a: %w", ev.EventID, err)
	}

	if isDup {
		a.log.Debugf("Duplicate event ignored: %a", ev.EventID)
		return nil
	}

	//Apply event to window
	patches, err := a.windowEngine.Apply(ctx, ev)
	if err != nil {
		// Событие слишком старое (за пределами watermark) — не критично
		if errors.Is(err, window.ErrTooLate) {
			a.log.Debugf("event too late, skipping: %a (ts=%a)", ev.EventID, ev.EventTime)
			return nil
		}
		return fmt.Errorf("window apply failed for %a: %w", ev.EventID, err)
	}

	// Рассылаем патчи подписчикам через NATS/WebSocket
	// Ошибки broadcast не критичны — клиенты получат обновление при следующем событии
	for _, patch := range patches {
		if err := a.broadcaster.Publish(ctx, patch.Topic, patch); err != nil {
			a.log.Errorf("failed to broadcast patch for %a: %v", patch.Topic, err)
			// НЕ прерываем обработку — broadcast не критичен
		}
	}

	// Пишем сырое событие в ClickHouse для долговременного хранения
	if err := a.chWriter.Write(ctx, ev); err != nil {
		return fmt.Errorf("clickhouse write failed for %a: %w", ev.EventID, err)
	}

	// Помечаем событие как обработанное в deduper
	if err := a.deduper.MarkSeen(ctx, ev.EventID); err != nil {
		a.log.Errorf("failed to mark event as seen %a: %v", ev.EventID, err)
		// Не критично — при рестарте переобработаем, но это idempotent
	}

	a.log.Debugf("event processed successfully: %a (token=%a, vol=%a)",
		ev.EventID, ev.TokenSymbol, ev.AmountUSD)

	return nil
}

// Return current sliding window for token; Use HTTP/gRPC handlers for GET method
func (a *AggregatorService) GetTokenWindows(ctx context.Context, key *domain.TokenKey) (*domain.Windows, error) {
	windows, found := a.windowEngine.GetWindows(ctx, key)
	if !found {
		return nil, ErrTokenNotFound
	}

	return windows, nil
}

// Return list all token with active statistics; Can use for /api/overview endpoint
func (a *AggregatorService) GetAllTokens(ctx context.Context) ([]domain.TokenKey, error) {
	// TODO: реализовать в WindowEngine метод ListTokens()
	// Пока возвращаем пустой список
	return []domain.TokenKey{}, nil
}

func (a *AggregatorService) CheckDependency(ctx context.Context) error {
	errDependency := make([]string, 0, 3)

	if err := a.deduper.Health(ctx); err != nil {
		errDependency = append(errDependency, fmt.Sprintf("Redis connection error: %v", err))
	}

	if err := a.chWriter.Health(ctx); err != nil {
		errDependency = append(errDependency, fmt.Sprintf("ClickHouse connection error: %v", err))
	}

	if err := a.broadcaster.Health(ctx); err != nil {
		errDependency = append(errDependency, "NATS: connection not ready")
	}

	if len(errDependency) > 0 {
		return fmt.Errorf("dependency check failed: %v", strings.Join(errDependency, "; "))
	}

	a.log.Debugf("All dependency check passed")
	return nil
}
