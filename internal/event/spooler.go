package event

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

var ErrQueueFull = errors.New("event spooler queue is full")

type Event struct {
	Payload    []byte
	ReceivedAt time.Time
}

type Spooler struct {
	queue   chan Event
	workers int
	logger  *slog.Logger
	wg      sync.WaitGroup
	stopCh  chan struct{}
}

func NewSpooler(bufferSize int, workerCount int, logger *slog.Logger) *Spooler {
	return &Spooler{
		queue:   make(chan Event, bufferSize),
		workers: workerCount,
		logger:  logger,
		stopCh:  make(chan struct{}),
	}
}

func (s *Spooler) Start() {
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}
}

func (s *Spooler) Enqueue(ctx context.Context, e Event) error {
	select {
	case s.queue <- e:
		return nil
	default:
		return ErrQueueFull
	}
}

func (s *Spooler) worker(id int) {
	defer s.wg.Done()
	for {
		select {
		case evt := <-s.queue:
			s.logger.Info("processing event", "worker_id", id, "size", len(evt.Payload))
			time.Sleep(100 * time.Millisecond)
		case <-s.stopCh:
			return
		}
	}
}

func (s *Spooler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}
