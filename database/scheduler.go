package database

import "time"

type DBMaintenanceScheduler struct {
	engine *DatabaseEngine

	checkpoint *time.Ticker
	truncate   *time.Ticker
	vacuum     *time.Ticker
	stop       chan struct{}
}

func NewDBMaintenanceScheduler(e *DatabaseEngine) *DBMaintenanceScheduler {
	return &DBMaintenanceScheduler{
		engine: e,
		stop:   make(chan struct{}),
	}
}

func (s *DBMaintenanceScheduler) Start() {
	s.checkpoint = time.NewTicker(2 * time.Minute)
	s.truncate = time.NewTicker(15 * time.Minute)
	s.vacuum = time.NewTicker(2 * time.Hour)

	go func() {
		for {
			select {
			case <-s.checkpoint.C:
				if !s.engine.isWriteHot(30 * time.Second) {
					s.engine.checkpoint(false)
				}
			case <-s.truncate.C:
				if !s.engine.isWriteHot(30 * time.Second) {
					s.engine.checkpoint(true)
				}
			case <-s.vacuum.C:
				if !s.engine.isWriteHot(30 * time.Second) {
					s.engine.maybeVacuum(0.25)
				}
			case <-s.stop:
				return
			}
		}
	}()
}

func (s *DBMaintenanceScheduler) Stop() {
	close(s.stop)
	if s.checkpoint != nil {
		s.checkpoint.Stop()
	}
	if s.truncate != nil {
		s.truncate.Stop()
	}
	if s.vacuum != nil {
		s.vacuum.Stop()
	}
}
