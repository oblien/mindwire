package agent

// TaskInfo reports native work whose lifetime can extend past the foreground
// response. A launch acknowledgement is not a completed task.
type TaskInfo struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Title    string `json:"title,omitempty"`
	Status   string `json:"status"`
	Output   string `json:"output,omitempty"`
}

type TaskTracker struct{ tasks map[string]TaskInfo }

func (t *TaskTracker) Update(task TaskInfo, start bool) bool {
	if task.ID == "" {
		return false
	}
	if t.tasks == nil {
		t.tasks = map[string]TaskInfo{}
	}
	old, exists := t.tasks[task.ID]
	if exists && old.Status != "running" && task.Status == "running" && !start {
		return false
	}
	if task.Title == "" {
		task.Title = old.Title
	}
	if task.Kind == "" {
		task.Kind = old.Kind
	}
	if task.ParentID == "" {
		task.ParentID = old.ParentID
	}
	if task.Output == "" && !start {
		task.Output = old.Output
	}
	t.tasks[task.ID] = task
	return true
}
func (t *TaskTracker) Get(id string) TaskInfo { return t.tasks[id] }
func (t *TaskTracker) Active() bool {
	for _, task := range t.tasks {
		if task.Status == "running" {
			return true
		}
	}
	return false
}
