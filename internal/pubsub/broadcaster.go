package pubsub

import "context"

type Broadcaster interface {
	Publish(ctx context.Context, subject string, data interface{}) error
	Health(ctx context.Context) error
}
