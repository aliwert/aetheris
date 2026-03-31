package resilience

import (
	"context"
	"math"
	"math/rand"
	"time"
)

type RetryConfig struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	JitterFactor    float64
}

type RetryFunc func(ctx context.Context, attempt int) error

func Do(ctx context.Context, cfg RetryConfig, fn RetryFunc) error {
	var lastErr error
	interval := float64(cfg.InitialInterval)

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = fn(ctx, attempt)
		if lastErr == nil {
			return nil
		}

		if attempt >= cfg.MaxAttempts {
			break
		}

		cappedInterval := math.Min(interval, float64(cfg.MaxInterval))
		jitter := (rand.Float64()*2 - 1) * cfg.JitterFactor * cappedInterval
		sleep := time.Duration(cappedInterval + jitter)
		if sleep < 0 {
			sleep = 0
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}

		interval *= cfg.Multiplier
	}
	return lastErr
}
