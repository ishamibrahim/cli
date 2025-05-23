// Copyright 2018. Akamai Technologies, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akamai/cli/v2/pkg/color"
	"github.com/akamai/cli/v2/pkg/log"
	"github.com/akamai/cli/v2/pkg/tools"
	"github.com/akamai/cli/v2/pkg/version"
	"github.com/urfave/cli/v2"
)

func (l *langManager) installGolang(ctx context.Context, dir, ver string, commands, ldFlags []string) error {
	logger := log.FromContext(ctx)

	goBin, err := l.commandExecutor.LookPath("go")
	if err != nil {
		logger.Error("Go executable not found")
		return fmt.Errorf("%w: %s. Please verify if the executable is included in your PATH", ErrRuntimeNotFound, "go")
	}

	logger.Debug(fmt.Sprintf("Go binary found: %s", goBin))

	if ver != "" && ver != "*" {
		cmd := exec.Command(goBin, "version")
		output, _ := l.commandExecutor.ExecCommand(cmd)
		logger.Debug(fmt.Sprintf("%s version: %s", goBin, bytes.ReplaceAll(output, []byte("\n"), []byte(""))))
		r := regexp.MustCompile("go version go(.*?) .*")
		matches := r.FindStringSubmatch(string(output))

		if len(matches) == 0 {
			logger.Error(fmt.Sprintf("Unable to determine Go version: %s", string(output)))
			return fmt.Errorf("%w: %s:%s", ErrRuntimeNoVersionFound, "go", ver)
		}

		if version.Compare(ver, matches[1]) == version.Greater {
			logger.Debug(fmt.Sprintf("Go Version found: %s", matches[1]))
			return fmt.Errorf("%w: required: %s:%s, have: %s. Please upgrade your runtime", ErrRuntimeMinimumVersionRequired, "go", ver, matches[1])
		}
	}

	cliPath, err := tools.GetAkamaiCliPath()
	if err != nil {
		return cli.Exit(color.RedString("Unable to determine CLI home directory"), 1)
	}

	if goPath := os.Getenv("GOPATH"); goPath != "" {
		cliPath = fmt.Sprintf("%s%d%s", goPath, os.PathListSeparator, cliPath)
	}

	if err := os.Setenv("GOPATH", cliPath); err != nil {
		logger.Error(fmt.Sprintf("Unable to set GOPATH: %v", err))
		return err
	}

	if err = installGolangModules(logger, l.commandExecutor, dir); err != nil {
		return err
	}

	if len(commands) != len(ldFlags) {
		return fmt.Errorf("commands and ldFlags should have the same length")
	}

	for n, command := range commands {
		ldFlag := ldFlags[n]
		execName := "akamai-" + strings.ToLower(command)

		var cmd *exec.Cmd
		params := []string{"build", "-o", execName}
		if ldFlag != "" {
			params = append(params, fmt.Sprintf(`-ldflags=%s`, ldFlag))
		}
		if len(commands) > 1 {
			params = append(params, "./"+command)
		} else {
			params = append(params, ".")
		}
		cmd = exec.Command(goBin, params...)

		cmd.Dir = dir
		logger.Debug(fmt.Sprintf("building with command: %+v", cmd))
		_, err = l.commandExecutor.ExecCommand(cmd)
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				logger.Debug(fmt.Sprintf("Unable to build binary (%s): \n%s", execName, exitErr.Stderr))
			}
			return fmt.Errorf("%w: %s", ErrPackageCompileFailure, command)
		}
	}

	return nil
}

func installGolangModules(logger *slog.Logger, cmdExecutor executor, dir string) error {
	bin, err := cmdExecutor.LookPath("go")
	if err != nil {
		err = fmt.Errorf("%w: %s. Please verify if the executable is included in your PATH", ErrRuntimeNotFound, "go")
		logger.Debug(err.Error())
		return err
	}
	if ok, _ := cmdExecutor.FileExists(filepath.Join(dir, "go.sum")); !ok {
		dep, _ := cmdExecutor.FileExists(filepath.Join(dir, "Gopkg.lock"))
		if !dep {
			return fmt.Errorf("go.sum not found, unable to initialize go modules due to lack of Gopkg.lock")
		}
		logger.Debug("go.sum not found, attempting go mod init")
		moduleName := filepath.Base(dir)
		cmd := exec.Command(bin, "mod", "init", moduleName)
		cmd.Dir = dir
		_, err = cmdExecutor.ExecCommand(cmd)
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				logger.Debug(fmt.Sprintf("Unable execute 'go mod init': \n %s", exitErr.Stderr))
			}
			return fmt.Errorf("%w: %s", ErrPackageManagerExec, "go mod init")
		}
	}
	logger.Info("go.sum found, running go module package manager")
	cmd := exec.Command(bin, "mod", "tidy")
	cmd.Dir = dir
	_, err = cmdExecutor.ExecCommand(cmd)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			logger.Debug(fmt.Sprintf("Unable execute 'go mod tidy': \n %s", exitErr.Stderr))
		}
		return fmt.Errorf("%w: %s", ErrPackageManagerExec, "go mod")
	}
	return nil
}
