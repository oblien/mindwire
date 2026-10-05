package registry

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

const ProjectLibraryVersion = 1

type ProjectFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProjectLibrary is presentation metadata. None of its operations move files or
// touch native harness transcripts. Order and membership use this registry's project IDs.
type ProjectLibrary struct {
	Version    int               `json:"version"`
	Revision   int64             `json:"revision"`
	Folders    []ProjectFolder   `json:"folders"`
	Membership map[string]string `json:"membership"`
	Order      []string          `json:"order"`
}

type ProjectPlacement struct {
	ProjectID string  `json:"projectId"`
	FolderID  *string `json:"folderId"` // null returns a project to the flat list
}

// Edits are partial, atomic and conditional. Retrying an already applied edit
// succeeds without a new revision; a stale, different intent returns ErrConflict.
type ProjectLibraryEdit struct {
	ExpectedRevision int64              `json:"expectedRevision"`
	Folders          []ProjectFolder    `json:"folders,omitempty"`
	DeleteFolders    []string           `json:"deleteFolders,omitempty"`
	Placements       []ProjectPlacement `json:"placements,omitempty"`
	Order            []string           `json:"order,omitempty"`
	FolderOrder      []string           `json:"folderOrder,omitempty"`
	ImportIfEmpty    bool               `json:"importIfEmpty,omitempty"`
}

func emptyLibrary() ProjectLibrary {
	return ProjectLibrary{Version: ProjectLibraryVersion, Folders: []ProjectFolder{}, Membership: map[string]string{}, Order: []string{}}
}

func readLibrary(tx *sql.Tx) (ProjectLibrary, error) {
	library := emptyLibrary()
	var data []byte
	err := tx.QueryRow("SELECT data FROM project_library WHERE id=1").Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return library, nil
	}
	if err != nil {
		return library, err
	}
	err = json.Unmarshal(data, &library)
	return library, err
}

func saveLibrary(tx *sql.Tx, library ProjectLibrary, revision int64) error {
	library.Revision = revision
	data, err := json.Marshal(library)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO project_library(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", data)
	return err
}

// ProjectLibrary reads only organization metadata, without enumerating projects
// or native conversations. It shares the registry's transaction boundary.
func (st *Store) ProjectLibrary() (ProjectLibrary, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return ProjectLibrary{}, err
	}
	defer tx.Rollback()
	library, err := readLibrary(tx)
	if err != nil {
		return library, err
	}
	return library, tx.Commit()
}

func libraryOrder(base, requested []string) ([]string, error) {
	seen := map[string]bool{}
	for _, id := range requested {
		if !validID(id) || seen[id] {
			return nil, invalid("duplicate or invalid ordered ID")
		}
		seen[id] = true
	}
	base = slices.Clone(base)
	for _, id := range requested {
		if !slices.Contains(base, id) {
			base = append(base, id)
		}
	}
	index := 0
	for i, id := range base {
		if seen[id] {
			base[i] = requested[index]
			index++
		}
	}
	return base, nil
}

func (st *Store) EditProjectLibrary(edit ProjectLibraryEdit) error {
	if edit.ExpectedRevision < 0 || len(edit.Folders)+len(edit.DeleteFolders)+len(edit.Placements)+len(edit.Order)+len(edit.FolderOrder) > 20000 {
		return invalid("project library edit is too large or has an invalid revision")
	}
	return st.write(func(tx *sql.Tx, revision int64) (bool, error) {
		before, err := readLibrary(tx)
		if err != nil {
			return false, err
		}
		if edit.ImportIfEmpty && before.Revision != 0 {
			return false, nil
		}
		next := before
		next.Folders = slices.Clone(before.Folders)
		next.Membership = map[string]string{}
		for k, v := range before.Membership {
			next.Membership[k] = v
		}
		deleted := map[string]bool{}
		newDeletion := false
		for _, id := range edit.DeleteFolders {
			if !validID(id) {
				return false, invalid("folder ID")
			}
			deleted[id] = true
			var removed int
			if err := tx.QueryRow("SELECT count(*) FROM project_folder_deletions WHERE id=?", id).Scan(&removed); err != nil {
				return false, err
			}
			newDeletion = newDeletion || removed == 0
		}
		next.Folders = slices.DeleteFunc(next.Folders, func(f ProjectFolder) bool { return deleted[f.ID] })
		for id, folder := range next.Membership {
			if deleted[folder] {
				delete(next.Membership, id)
			}
		}
		for _, folder := range edit.Folders {
			folder.Name = strings.TrimSpace(folder.Name)
			if !validID(folder.ID) || deleted[folder.ID] || folder.Name == "" || utf8.RuneCountInString(folder.Name) > 80 || strings.ContainsAny(folder.Name, "\x00\r\n") {
				return false, invalid("folder name or ID")
			}
			index := slices.IndexFunc(next.Folders, func(f ProjectFolder) bool { return f.ID == folder.ID })
			var removed int
			if err := tx.QueryRow("SELECT count(*) FROM project_folder_deletions WHERE id=?", folder.ID).Scan(&removed); err != nil {
				return false, err
			}
			if removed > 0 {
				return false, ErrDeleted
			}
			if index >= 0 {
				next.Folders[index] = folder
			} else {
				next.Folders = append(next.Folders, folder)
			}
		}
		folders := map[string]ProjectFolder{}
		for i, folder := range next.Folders {
			for _, other := range next.Folders[:i] {
				if strings.EqualFold(folder.Name, other.Name) {
					return false, invalid("a folder with this name already exists")
				}
			}
			folders[folder.ID] = folder
		}
		for _, placement := range edit.Placements {
			if !validID(placement.ProjectID) {
				return false, invalid("project ID")
			}
			if placement.FolderID == nil {
				delete(next.Membership, placement.ProjectID)
				continue
			}
			if _, exists := folders[*placement.FolderID]; !exists {
				return false, invalid("folder no longer exists")
			}
			project, _, err := record(tx, "projects", placement.ProjectID)
			if err != nil {
				return false, err
			}
			if project == nil {
				return false, invalid("project no longer exists")
			}
			next.Membership[placement.ProjectID] = *placement.FolderID
		}
		for _, id := range edit.Order {
			project, _, err := record(tx, "projects", id)
			if err != nil {
				return false, err
			}
			if project == nil {
				return false, invalid("ordered project no longer exists")
			}
		}
		if len(edit.Order) > 0 {
			next.Order, err = libraryOrder(before.Order, edit.Order)
			if err != nil {
				return false, err
			}
		}
		if len(edit.FolderOrder) > 0 {
			ids := make([]string, 0, len(next.Folders))
			for _, f := range next.Folders {
				ids = append(ids, f.ID)
			}
			for _, id := range edit.FolderOrder {
				if _, ok := folders[id]; !ok {
					return false, invalid("ordered folder no longer exists")
				}
			}
			ids, err = libraryOrder(ids, edit.FolderOrder)
			if err != nil {
				return false, err
			}
			next.Folders = make([]ProjectFolder, 0, len(ids))
			for _, id := range ids {
				next.Folders = append(next.Folders, folders[id])
			}
		}
		if len(next.Folders) > 10000 {
			return false, invalid("too many project folders")
		}
		if reflect.DeepEqual(before, next) && !newDeletion {
			return false, nil
		}
		if edit.ExpectedRevision != before.Revision {
			return false, ErrConflict
		}
		for id := range deleted {
			if _, err := tx.Exec("INSERT OR IGNORE INTO project_folder_deletions(id) VALUES(?)", id); err != nil {
				return false, err
			}
		}
		return true, saveLibrary(tx, next, revision)
	})
}

func removeProjectPlacement(tx *sql.Tx, id string, revision int64) error {
	library, err := readLibrary(tx)
	if err != nil {
		return err
	}
	_, member := library.Membership[id]
	if !member && !slices.Contains(library.Order, id) {
		return nil
	}
	delete(library.Membership, id)
	library.Order = slices.DeleteFunc(library.Order, func(value string) bool { return value == id })
	return saveLibrary(tx, library, revision)
}
