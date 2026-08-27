package server

import (
	"log"
	"time"
)

const stagingCleanupInterval = time.Hour

func (s *Server) StartUploadMaintenance() {
	if s.staging == nil {
		return
	}
	s.uploadMaintenanceOnce.Do(func() {
		s.uploadMaintenanceStop = make(chan struct{})
		s.uploadMaintenanceWG.Add(1)
		go func() {
			defer s.uploadMaintenanceWG.Done()
			cleanup := func() {
				if _, err := s.staging.Cleanup(time.Now().UTC(), assetTokenLifetime); err != nil {
					log.Printf("upload staging cleanup failed: %v", err)
				}
			}
			cleanup()
			ticker := time.NewTicker(stagingCleanupInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					cleanup()
				case <-s.uploadMaintenanceStop:
					return
				}
			}
		}()
	})
}

func (s *Server) ShutdownUploadMaintenance() {
	s.uploadMaintenanceStopOnce.Do(func() {
		if s.uploadMaintenanceStop != nil {
			close(s.uploadMaintenanceStop)
		}
	})
	s.uploadMaintenanceWG.Wait()
}
