package consul

import (
	"fmt"
	"github.com/hashicorp/consul/consul/structs"
	"time"
)

func (s *Server) initializeSessionTimers() error {
	s.sessionTimersLock.Lock()
	s.sessionTimers = make(map[string]*time.Timer)
	s.sessionTimersLock.Unlock()

	// walk the TTL index and resetSessionTimer for each non-zero TTL
	state := s.fsm.State()
	_, sessions, err := state.SessionListTTL()
	if err != nil {
		return err
	}
	for _, session := range sessions {
		err := s.resetSessionTimer(session.ID, session)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) resetSessionTimer(id string, session *structs.Session) error {
	if session == nil {
		var err error

		// find the session
		state := s.fsm.State()
		_, session, err = state.SessionGet(id)
		if err != nil || session == nil {
			return fmt.Errorf("Could not find session for '%s'\n", id)
		}
	}

	if session.TTL != "" {
		ttl, err := time.ParseDuration(session.TTL)
		if err != nil {
			return fmt.Errorf("Invalid Session TTL '%s': %v", session.TTL, err)
		}
		s.sessionTimersLock.Lock()
		if s.sessionTimers == nil {
			// should not happen . . .
			panic(fmt.Sprintf("Invalid call to resetSessionTimer before creation of session timers in leaderLoop"))
		}
		defer s.sessionTimersLock.Unlock()
		if t := s.sessionTimers[id]; t != nil {
			// TBD may modify the session's active TTL based on load here
			t.Reset(ttl * structs.SessionTTLMultiplier)
		} else {
			s.sessionTimers[session.ID] = time.AfterFunc(ttl*structs.SessionTTLMultiplier, func() {
				s.sessionTimers[session.ID].Stop()
				args := structs.SessionRequest{
					Datacenter: s.config.Datacenter,
					Op:         structs.SessionDestroy,
				}
				args.Session.ID = session.ID

				// Apply the update to destroy the session
				_, err := s.raftApply(structs.SessionRequestType, args)
				if err != nil {
					s.logger.Printf("[ERR] consul.session: Apply failed: %v", err)
				}
			})
		}
	}
	return nil
}

func (s *Server) clearSessionTimer(id string) error {
	s.sessionTimersLock.Lock()
	defer s.sessionTimersLock.Unlock()
	if s.sessionTimers[id] != nil {
		// stop the session timer and delete from the map
		s.sessionTimers[id].Stop()
		delete(s.sessionTimers, id)
	}
	return nil
}

func (s *Server) clearAllSessionTimers() error {
	s.sessionTimersLock.Lock()
	defer s.sessionTimersLock.Unlock()

	// stop all timers and clear out the map
	for id, t := range s.sessionTimers {
		t.Stop()
		delete(s.sessionTimers, id)
	}
	return nil
}
