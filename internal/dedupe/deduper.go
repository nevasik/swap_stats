package dedupe

import "context"

// General contact deduping(redis, in-memory, bloom, etc.)
type Deduper interface {
	// if alreadySeen=true -> duplicate, request processing can be skip
	Seen(ctx context.Context, id string) (alreadySeen bool, err error)
}
