package registry

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type AgentInstallation struct {
	AgentType string
	Name      string
	State     string // installed | missing | repair; failed observations are omitted
}

type AgentDiscoveryIssue struct {
	Agent   string `json:"agent"`
	Message string `json:"message"`
}

type EnsureAgentRequest struct {
	AgentType string `json:"agentType"`
	Name      string `json:"name,omitempty"`
}

type EnsureAgentResult struct {
	AgentID  string   `json:"agentId"`
	Snapshot Snapshot `json:"snapshot"`
}

func agentProfiles(tx *sql.Tx) ([]Agent, error) {
	rows, err := tx.Query("SELECT data FROM agents ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var profiles []Agent
	for rows.Next() {
		var data []byte
		var profile Agent
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &profile); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

// EnsureAgent is setup admission, not persona creation. All clients opening setup
// for one harness reuse its existing profile, including one discovered from native
// chats. Selection matches the native-session index; old names, mutes and IDs win.
func (st *Store) EnsureAgent(input EnsureAgentRequest, catalogName string) (Agent, error) {
	var profile Agent
	err := st.write(func(tx *sql.Tx, revision int64) (bool, error) {
		profiles, err := agentProfiles(tx)
		if err != nil {
			return false, err
		}
		for _, existing := range profiles {
			if existing.AgentType == input.AgentType {
				profile = existing
				return false, nil
			}
		}
		name := strings.TrimSpace(input.Name)
		if name == "" {
			name = catalogName
		}
		profile = Agent{Record: Record{ID: nativeID(st.identity, "setup", input.AgentType, strconv.FormatInt(revision, 10)),
			WorkspaceID: st.identity, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Revision: revision},
			Name: name, AgentType: input.AgentType, AgentTypeName: catalogName}
		data, _ := json.Marshal(profile)
		data, _, _, err = st.normalize("agents", profile.ID, data, revision)
		if err != nil {
			return false, err
		}
		return true, save(tx, "agents", profile.ID, data, revision, "", "")
	})
	return profile, err
}

// SyncAgentInstallations projects real executable presence into the existing cache.
// One transaction creates missing profiles and updates observations. It never
// removes native data, renames a profile, remaps chat parents, or writes on no change.
func (st *Store) SyncAgentInstallations(observations []AgentInstallation) error {
	if len(observations) == 0 {
		return nil
	}
	return st.write(func(tx *sql.Tx, revision int64) (bool, error) {
		profiles, err := agentProfiles(tx)
		if err != nil {
			return false, err
		}
		byType := map[string][]Agent{}
		for _, profile := range profiles {
			byType[profile.AgentType] = append(byType[profile.AgentType], profile)
		}
		seen := map[string]bool{}
		changed := false
		for _, observation := range observations {
			if seen[observation.AgentType] || !validID(observation.AgentType) || observation.Name == "" ||
				(observation.State != "installed" && observation.State != "missing" && observation.State != "repair") {
				return false, invalid("harness installation observation")
			}
			seen[observation.AgentType] = true
			matching := byType[observation.AgentType]
			if len(matching) == 0 && observation.State != "missing" {
				id := nativeID(st.identity, "installed", observation.AgentType)
				gone, err := isDeleted(tx, "agents", id)
				if err != nil {
					return false, err
				}
				// Explicitly removed discovered profiles stay removed. Opening setup
				// again creates a fresh profile; it never resurrects deleted chats.
				if gone {
					continue
				}
				matching = []Agent{{Record: Record{ID: id, WorkspaceID: st.identity,
					CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)},
					Name: observation.Name, AgentTypeName: observation.Name, AgentType: observation.AgentType}}
			}
			for _, profile := range matching {
				if profile.Installation == observation.State {
					continue
				}
				profile.Installation, profile.Revision = observation.State, revision
				data, _ := json.Marshal(profile)
				if err := save(tx, "agents", profile.ID, data, revision, "", ""); err != nil {
					return false, err
				}
				changed = true
			}
		}
		return changed, nil
	})
}
