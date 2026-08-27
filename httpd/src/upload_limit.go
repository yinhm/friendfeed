package server

import "sync"

func (s *Server) beginUpload(actor string, image bool) (func(), bool) {
	select {
	case s.uploadRequests <- struct{}{}:
	default:
		return nil, false
	}
	s.uploadUsersMu.Lock()
	if s.uploadUsers == nil {
		s.uploadUsers = make(map[string]int)
	}
	if s.uploadUsers[actor] >= 2 {
		s.uploadUsersMu.Unlock()
		<-s.uploadRequests
		return nil, false
	}
	s.uploadUsers[actor]++
	s.uploadUsersMu.Unlock()
	if image {
		select {
		case s.imageOperations <- struct{}{}:
		default:
			s.finishUpload(actor, false)
			return nil, false
		}
	}
	var once sync.Once
	return func() { once.Do(func() { s.finishUpload(actor, image) }) }, true
}

func (s *Server) finishUpload(actor string, image bool) {
	if image {
		<-s.imageOperations
	}
	s.uploadUsersMu.Lock()
	s.uploadUsers[actor]--
	if s.uploadUsers[actor] == 0 {
		delete(s.uploadUsers, actor)
	}
	s.uploadUsersMu.Unlock()
	<-s.uploadRequests
}
