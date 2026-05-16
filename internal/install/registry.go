package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var ErrNoGitToplevel = errors.New("opsmask: not inside a git project")

type registryFile struct {
	Projects []string `json:"projects"`
}

func ResolveProjectToplevel(cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	cmd := osexec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: OpsMask hook requires a git project. Run `git init` in this project, then re-run install", ErrNoGitToplevel)
	}
	top := strings.TrimSpace(string(out))
	real, err := filepath.EvalSymlinks(top)
	if err != nil {
		return "", err
	}
	return real, nil
}

func RegisterInstall(projectToplevel string) error {
	return updateRegistry(func(r *registryFile) {
		for _, p := range r.Projects {
			if p == projectToplevel {
				return
			}
		}
		r.Projects = append(r.Projects, projectToplevel)
		sort.Strings(r.Projects)
	})
}

func Unregister(projectToplevel string) error {
	return updateRegistry(func(r *registryFile) {
		out := r.Projects[:0]
		for _, p := range r.Projects {
			if p != projectToplevel {
				out = append(out, p)
			}
		}
		r.Projects = out
	})
}

func IsRegistered(projectToplevel string) (bool, error) {
	r, err := readRegistry()
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, p := range r.Projects {
		if p == projectToplevel {
			return true, nil
		}
	}
	return false, nil
}

func updateRegistry(fn func(*registryFile)) error {
	r, err := readRegistry()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	fn(&r)
	return writeRegistry(r)
}

func readRegistry() (registryFile, error) {
	path, err := registryPath()
	if err != nil {
		return registryFile{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return registryFile{}, err
	}
	if info.Mode().Perm() != 0o600 {
		return registryFile{}, fmt.Errorf("%s must have mode 0600", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return registryFile{}, err
	}
	var r registryFile
	if len(body) > 0 {
		if err := json.Unmarshal(body, &r); err != nil {
			return registryFile{}, err
		}
	}
	return r, nil
}

func writeRegistry(r registryFile) error {
	path, err := registryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hook_installs.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func registryPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "opsmask", "hook_installs.json"), nil
}
