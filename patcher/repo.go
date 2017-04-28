package patcher

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	modulePrefix          = "path = "
	submoduleMessageRegex = `^.*is in submodule '(.*)'`
)

type commandRunner interface {
	Run(command Command) (err error)
	CombinedOutput(command Command) ([]byte, error)
}

type Repo struct {
	runner         commandRunner
	repo           string
	committerName  string
	committerEmail string
}

func NewRepo(commandRunner commandRunner, repo string, committerName, committerEmail string) Repo {
	return Repo{
		runner:         commandRunner,
		repo:           repo,
		committerName:  committerName,
		committerEmail: committerEmail,
	}
}

func (r Repo) Checkout(checkoutRef string) error {
	commands := []Command{
		Command{
			Args: []string{"checkout", checkoutRef},
			Dir:  r.repo,
		},
		Command{
			Args: []string{"clean", "-ffd"},
			Dir:  r.repo,
		},
		Command{
			Args: []string{"submodule", "init"},
			Dir:  r.repo,
		},
		Command{
			Args: []string{"submodule", "foreach", "--recursive", "git submodule sync"},
			Dir:  r.repo,
		},
		Command{
			Args: []string{"submodule", "update", "--init", "--recursive", "--force", "--jobs=4"},
			Dir:  r.repo,
		},
		Command{
			Args: []string{"submodule", "foreach", "--recursive", "git clean -ffd"},
			Dir:  r.repo,
		},
	}

	for _, command := range commands {
		if err := r.runner.Run(command); err != nil {
			return err
		}
	}

	return nil
}

func (r Repo) ApplyPatch(patch string) error {
	command := Command{
		Args: []string{"am", patch},
		Dir:  r.repo,
	}

	err := r.runner.Run(command)
	if err != nil {
		return err
	}

	return nil
}

func (r Repo) AddSubmodule(path, url, ref, branch string) error {
	var submoduleAddArgs []string
	pathToSubmodule := filepath.Join(r.repo, path)

	if branch != "" {
		submoduleAddArgs = []string{"submodule", "add", "--force", "-b", branch, url, path}
	} else {
		submoduleAddArgs = []string{"submodule", "add", "--force", url, path}
	}

	commands := []Command{
		Command{
			Args: submoduleAddArgs,
			Dir:  r.repo,
		},
		Command{
			Args: []string{"checkout", ref},
			Dir:  pathToSubmodule,
		},
		Command{
			Args: []string{"submodule", "foreach", "--recursive", "git submodule sync"},
			Dir:  pathToSubmodule,
		},
		Command{
			Args: []string{"submodule", "update", "--init", "--recursive", "--force", "--jobs=4"},
			Dir:  pathToSubmodule,
		},
		Command{
			Args: []string{"submodule", "foreach", "--recursive", "git clean -ffd"},
			Dir:  r.repo,
		},
		Command{
			Args: []string{"clean", "-ffd"},
			Dir:  pathToSubmodule,
		},
		Command{
			Args: []string{"add", "-A", path},
			Dir:  r.repo,
		},
		Command{
			Args: []string{
				"-c", fmt.Sprintf("user.name=%s", r.committerName),
				"-c", fmt.Sprintf("user.email=%s", r.committerEmail),
				"commit",
				"-m", fmt.Sprintf("Knit addition of %s", path),
				"--no-verify",
			},
			Dir: r.repo,
		},
	}

	for _, command := range commands {
		if err := r.runner.Run(command); err != nil {
			return err
		}
	}

	return nil
}

func (r Repo) RemoveSubmodule(path string) error {
	submoduleDeinitArgs := []string{"submodule", "deinit", "-f", path}
	submoduleRemoveArgs := []string{"rm", "-f", path}

	commands := []Command{
		Command{
			Args: submoduleDeinitArgs,
			Dir:  r.repo,
		},
		Command{
			Args: submoduleRemoveArgs,
			Dir:  r.repo,
		},
		Command{
			Args: []string{
				"-c", fmt.Sprintf("user.name=%s", r.committerName),
				"-c", fmt.Sprintf("user.email=%s", r.committerEmail),
				"commit",
				"-m", fmt.Sprintf("Knit removal of submodule '%s'", path),
				"--no-verify",
			},
			Dir: r.repo,
		},
	}

	for _, command := range commands {
		if err := r.runner.Run(command); err != nil {
			return err
		}
	}

	return nil
}

func (r Repo) BumpSubmodule(path, sha string) error {
	pathToSubmodule := filepath.Join(r.repo, path)
	pathToRepo := r.repo

	re := regexp.MustCompile(`(src/.*)/(src/.*)`)
	matches := re.FindStringSubmatch(path)
	if len(matches) == 3 {
		pathToRepo = filepath.Join(r.repo, matches[1])
		path = matches[2]
	}

	commands := []Command{
		Command{
			Args: []string{"fetch"},
			Dir:  pathToSubmodule,
		},
		Command{
			Args: []string{"checkout", sha},
			Dir:  pathToSubmodule,
		},
		Command{
			Args: []string{"submodule", "init"},
			Dir:  pathToSubmodule,
		},
		Command{
			Args: []string{"submodule", "sync"},
			Dir:  pathToSubmodule,
		},
		Command{
			Args: []string{"submodule", "update", "--init", "--recursive", "--force", "--jobs=4"},
			Dir:  pathToSubmodule,
		},
		Command{
			Args: []string{"submodule", "foreach", "--recursive", "git clean -ffd"},
			Dir:  r.repo,
		},
		Command{
			Args: []string{"clean", "-ffd"},
			Dir:  pathToSubmodule,
		},
		Command{
			Args: []string{"add", "-A", path},
			Dir:  pathToRepo,
		},
		Command{
			Args: []string{
				"-c", fmt.Sprintf("user.name=%s", r.committerName),
				"-c", fmt.Sprintf("user.email=%s", r.committerEmail),
				"commit",
				"-m", fmt.Sprintf("Knit bump of %s", path),
				"--no-verify",
			},
			Dir: pathToRepo,
		},
	}

	if len(matches) == 3 {
		commands = append(commands, Command{
			Args: []string{"add", "-A", matches[1]},
			Dir:  r.repo,
		}, Command{
			Args: []string{
				"-c", fmt.Sprintf("user.name=%s", r.committerName),
				"-c", fmt.Sprintf("user.email=%s", r.committerEmail),
				"commit",
				"-m", fmt.Sprintf("Knit bump of %s", matches[1]),
				"--no-verify",
			},
			Dir: r.repo,
		})
	}

	for _, command := range commands {
		if err := r.runner.Run(command); err != nil {
			return err
		}
	}

	return nil
}

func (r Repo) PatchSubmodule(path, fullPathToPatch string) error {
	applyCommand := Command{
		Args: []string{"am", fullPathToPatch},
		Dir:  filepath.Join(r.repo, path),
	}

	if err := r.runner.Run(applyCommand); err != nil {
		return err
	}

	addCommand := Command{
		Args: []string{"add", "-A", path},
		Dir:  r.repo,
	}

	if output, err := r.runner.CombinedOutput(addCommand); err != nil {
		re := regexp.MustCompile(submoduleMessageRegex)
		submodulePath := re.FindStringSubmatch(string(output))[1]
		absoluteSubmodulePath := filepath.Join(r.repo, submodulePath)

		commands := []Command{
			Command{
				Args: []string{"add", "-A", "."},
				Dir:  absoluteSubmodulePath,
			},
			Command{
				Args: []string{
					"-c", fmt.Sprintf("user.name=%s", r.committerName),
					"-c", fmt.Sprintf("user.email=%s", r.committerEmail),
					"commit",
					"-m", fmt.Sprintf("Knit submodule patch of %s", submodulePath),
					"--no-verify",
				},
				Dir: absoluteSubmodulePath,
			},
		}

		for _, command := range commands {
			if err := r.runner.Run(command); err != nil {
				return err
			}
		}
	}

	commitCommands := []Command{
		Command{
			Args: []string{"add", "-A", "."},
			Dir:  r.repo,
		},
		Command{
			Args: []string{
				"-c", fmt.Sprintf("user.name=%s", r.committerName),
				"-c", fmt.Sprintf("user.email=%s", r.committerEmail),
				"commit",
				"-m", fmt.Sprintf("Knit patch of %s", path),
				"--no-verify",
			},
			Dir: r.repo,
		},
	}

	for _, command := range commitCommands {
		if err := r.runner.Run(command); err != nil {
			return err
		}
	}

	return nil
}

func (r Repo) CheckoutBranch(name string) error {
	err := r.runner.Run(Command{
		Args: []string{"rev-parse", "--verify", fmt.Sprintf("refs/heads/%s", name)},
		Dir:  r.repo,
	})
	if err == nil {
		return fmt.Errorf("Branch %q already exists. Please delete it before trying again", name)
	}

	err = r.runner.Run(Command{
		Args: []string{"checkout", "-b", name},
		Dir:  r.repo,
	})
	if err != nil {
		return err
	}

	return nil
}

func (r Repo) submodules() ([]string, error) {
	modules, err := ioutil.ReadFile(filepath.Join(r.repo, ".gitmodules"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, err
	}

	var modulePaths []string
	lines := strings.Split(string(modules), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, modulePrefix) {
			modulePaths = append(modulePaths, strings.TrimPrefix(line, modulePrefix))
		}
	}

	var paths []string
	for _, modulePath := range modulePaths {
		fullModulePath := filepath.Join(r.repo, modulePath)
		_, err := os.Stat(fullModulePath)
		if os.IsNotExist(err) {
			continue
		}

		paths = append(paths, fullModulePath)
	}

	return paths, nil
}
