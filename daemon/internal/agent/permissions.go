package agent

// Labels describe native values; adapters still discover which values are supported.
func PermissionLabel(value string) string {
	switch value {
	case "default", "manual", "on-request":
		return "Ask when needed"
	case "untrusted":
		return "Ask before commands"
	case "acceptEdits":
		return "Allow file edits"
	case "bypassPermissions":
		return "Allow all"
	case "never":
		return "Never ask"
	case "dontAsk":
		return "Reject actions that need approval"
	case "auto", "auto_review":
		return "Approve for me"
	case "guardian_subagent":
		return "Approve for me (legacy)"
	case "user":
		return "Ask me"
	case "plan":
		return "Plan first"
	case "danger-full-access":
		return "Full access"
	case "workspace-write":
		return "Workspace access"
	case "read-only":
		return "Read only"
	default:
		return value
	}
}
