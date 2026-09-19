package registry

import "fmt"

func (st *Store) InteractionCWD(chatID, agentType, cwd string) (string, error) {
	chat, profile, project, err := st.ChatContext(chatID)
	if err != nil {
		return "", err
	}
	if chat != nil {
		if profile.AgentType != agentType {
			return "", fmt.Errorf("this chat now uses a different agent")
		}
		cwd = project.Path
	}
	if err := st.CheckProjectPath(cwd); err != nil {
		return "", err
	}
	return cwd, nil
}
