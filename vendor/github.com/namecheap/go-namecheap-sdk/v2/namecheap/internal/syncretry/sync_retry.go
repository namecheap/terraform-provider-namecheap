package syncretry

import (
	"errors"
	"sync"
	"time"
)

var ErrRetry = errors.New("retry error")
var ErrRetryAttempts = errors.New("retry attempts error")

type Options struct {
	Delays []int
}

func NewSyncRetry(options *Options) *SyncRetry {
	return &SyncRetry{
		m:       &sync.Mutex{},
		options: options,
	}
}

type SyncRetry struct {
	m       *sync.Mutex
	options *Options
}

func (sq *SyncRetry) Do(f func() error) error {
	err := f()
	if err == nil {
		return nil
	}

	if !errors.Is(err, ErrRetry) {
		return err
	}

	sq.m.Lock()
	defer sq.m.Unlock()

	for _, delay := range sq.options.Delays {
		time.Sleep(time.Duration(delay) * time.Second)
		err = f()
		if err == nil {
			return nil
		}

		if errors.Is(err, ErrRetry) {
			continue
		}
		return err
	}

	return ErrRetryAttempts
}
