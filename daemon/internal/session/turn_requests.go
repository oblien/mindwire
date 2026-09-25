package session

import "errors"

type turnReceipt struct {
	RunID  string `json:"runId"`
	Digest string `json:"digest"`
}

var ErrTurnRequestConflict = errors.New("requestId already belongs to a different turn")

func (st *Store) TurnRequest(id, digest string) (Run, bool, error) {
	if id == "" {
		return Run{}, false, nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	receipt, ok := st.s.TurnRequests[id]
	if !ok {
		return Run{}, false, nil
	}
	if receipt.Digest != digest {
		return Run{}, false, ErrTurnRequestConflict
	}
	for _, run := range st.s.Runs {
		if run.ID == receipt.RunID {
			return run, true, nil
		}
	}
	// A missing/deleted run must never cause this already-accepted request to run again.
	return Run{}, false, ErrTurnRequestConflict
}

// SaveTurnStart commits the ingress message, run and optional retry receipt
// together, before the supervisor launches any harness operation.
func (st *Store) SaveTurnStart(run Run, message *Message, requestID, digest string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.s.TurnRequests == nil {
		st.s.TurnRequests = map[string]turnReceipt{}
	}
	if requestID != "" {
		if _, exists := st.s.TurnRequests[requestID]; exists {
			return ErrTurnRequestConflict
		}
		st.s.TurnRequests[requestID] = turnReceipt{RunID: run.ID, Digest: digest}
	}
	runs, messages := len(st.s.Runs), len(st.s.Messages)
	st.s.Runs = append(st.s.Runs, run)
	if message != nil {
		st.s.Messages = append(st.s.Messages, *message)
	}
	if err := st.save(); err != nil {
		st.s.Runs = st.s.Runs[:runs]
		st.s.Messages = st.s.Messages[:messages]
		if requestID != "" {
			delete(st.s.TurnRequests, requestID)
		}
		return err
	}
	return nil
}
