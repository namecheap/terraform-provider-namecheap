package syncretry

import (
	"errors"
	"sync"
	"time"
)

var RetryError = errors.New("retry error")
var RetryAttemptsError = errors.New("retry attempts error")

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

	if !errors.Is(err, RetryError) {
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

		if errors.Is(err, RetryError) {
			continue
		} else {
			return err
		}
	}

	return RetryAttemptsError
}
