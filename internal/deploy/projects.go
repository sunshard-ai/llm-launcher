package deploy

import (
	"fmt"
	"os"
	"path/filepath"

	"llmlauncher/internal/domain"
)

const archiveFile = ".launcher.archived"

type ProjectSummary struct {
	Dir     string `json:"dir"`
	Project string `json:"project,omitempty"`
	Runtime string `json:"runtime,omitempty"`
	Model   string `json:"model,omitempty"`
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
}

func isArchived(dir string, p domain.Plan) (bool, error) {
	b, err := readSmall(filepath.Join(dir, archiveFile))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if string(b) != fingerprint(p)+"\n" {
		return false, fmt.Errorf("archive marker does not match the deployment")
	}
	return true, nil
}

// Restore makes an archived plan available to Up again without starting it.
func Restore(dir string) error {
	p, err := ReadPlan(dir)
	if err != nil {
		return err
	}
	unlock, err := acquireLock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	if archived, err := isArchived(dir, p); err != nil || !archived {
		return err
	}
	return os.Remove(filepath.Join(dir, archiveFile))
}

// List inspects the selected directory and its direct children. It reports
// saved state only. Status probes Docker and the API for live state.
func List(dir string) ([]ProjectSummary, error) {
	root, err := canonicalDir(dir)
	if err != nil {
		return nil, err
	}
	projects := []ProjectSummary{}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return projects, nil
	}
	if err != nil {
		return nil, err
	}

	dirs := []string{root}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(root, entry.Name()))
		}
	}
	for _, path := range dirs {
		if _, err := os.Lstat(filepath.Join(path, ManifestFile)); os.IsNotExist(err) {
			continue
		}
		projects = append(projects, summarizeProject(path))
	}
	return projects, nil
}

func summarizeProject(path string) ProjectSummary {
	summary := ProjectSummary{Dir: path, State: "invalid"}
	p, err := ReadPlan(path)
	if err != nil {
		summary.Error = err.Error()
		return summary
	}
	summary.Project, summary.Runtime, summary.Model = p.Project, p.Runtime, APIModel(p)
	archived, err := isArchived(path, p)
	if err != nil {
		summary.Error = err.Error()
		return summary
	}
	if archived {
		summary.State = "archived"
		return summary
	}
	state, err := loadState(path, p)
	if err != nil {
		summary.Error = err.Error()
		return summary
	}
	summary.State = state.Status
	if summary.State == "" {
		summary.State = "planned"
	}
	return summary
}
