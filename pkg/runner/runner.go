package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	junit "github.com/joshdk/go-junit"

	"github.com/kubeshop/testkube/pkg/api/v1/testkube"
	"github.com/kubeshop/testkube/pkg/executor"
	"github.com/kubeshop/testkube/pkg/executor/content"
	"github.com/kubeshop/testkube/pkg/executor/output"
	"github.com/kubeshop/testkube/pkg/executor/secret"
)

type Params struct {
	Datadir string // RUNNER_DATADIR
}

func NewRunner() *MavenRunner {
	params := Params{
		Datadir: os.Getenv("RUNNER_DATADIR"),
	}

	runner := &MavenRunner{
		params: params,
	}

	return runner
}

type MavenRunner struct {
	params Params
}

func (r *MavenRunner) Run(execution testkube.Execution) (result testkube.ExecutionResult, err error) {
	// check that the datadir exists
	_, err = os.Stat(r.params.Datadir)
	if errors.Is(err, os.ErrNotExist) {
		return result, err
	}

	// the Gradle executor does not support files
	if execution.Content.IsFile() {
		return result.Err(fmt.Errorf("executor only support git-dir based tests")), nil
	}

	// check that pom.xml file exists
	directory := filepath.Join(r.params.Datadir, "repo", execution.Content.Repository.Path)
	pomXml := filepath.Join(directory, "pom.xml")

	_, pomXmlErr := os.Stat(pomXml)
	if errors.Is(pomXmlErr, os.ErrNotExist) {
		return result.Err(fmt.Errorf("no pom.xml found")), nil
	}

	// add configuration files
	err = content.PlaceFiles(execution.CopyFiles)
	if err != nil {
		return result.Err(fmt.Errorf("could not place config files: %w", err)), nil
	}

	// determine the Maven command to use
	mavenCommand := "mvn"
	mavenWrapper := filepath.Join(directory, "mvnw")
	_, err = os.Stat(mavenWrapper)
	if err == nil {
		// then we use the wrapper instead
		mavenCommand = "./mvnw"
	}

	secret.NewEnvManager().GetVars(execution.Variables)
	// simply set the ENVs to use during Maven execution
	for _, env := range execution.Variables {
		os.Setenv(env.Name, env.Value)
	}

	// pass additional executor arguments/flags to Gradle
	args := []string{}
	args = append(args, execution.Args...)

	if execution.VariablesFile != "" {
		settingsXML, err := createSettingsXML(directory, execution.VariablesFile)
		if err != nil {
			return result.Err(fmt.Errorf("could not create settings.xml")), nil
		}
		args = append(args, "--settings", settingsXML)
	}

	goal := strings.Split(execution.TestType, "/")[1]
	if !strings.EqualFold(goal, "project") {
		// use the test subtype as goal or phase when != project
		// in case of project there is need to pass additional args
		args = append(args, goal)
	}

	// workaround for https://github.com/eclipse/che/issues/13926
	os.Unsetenv("MAVEN_CONFIG")

	output.PrintEvent("Running", directory, mavenCommand, args)
	output, err := executor.Run(directory, mavenCommand, args...)

	if err == nil {
		result.Status = testkube.ExecutionStatusPassed
	} else {
		result.Status = testkube.ExecutionStatusFailed
		result.ErrorMessage = err.Error()
		if strings.Contains(result.ErrorMessage, "exit status 1") {
			// probably some tests have failed
			result.ErrorMessage = "build failed with an exception"
		} else {
			// Gradle was unable to run at all
			return result, nil
		}
	}

	result.Output = string(output)
	result.OutputType = "text/plain"

	junitReportPath := filepath.Join(directory, "target", "surefire-reports")
	err = filepath.Walk(junitReportPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".xml" {
			suites, _ := junit.IngestFile(path)
			for _, suite := range suites {
				for _, test := range suite.Tests {
					result.Steps = append(
						result.Steps,
						testkube.ExecutionStepResult{
							Name:     fmt.Sprintf("%s - %s", suite.Name, test.Name),
							Duration: test.Duration.String(),
							Status:   mapStatus(test.Status),
						})
				}
			}
		}

		return nil
	})

	if err != nil {
		return result.Err(err), nil
	}

	return result, nil
}

func mapStatus(in junit.Status) (out string) {
	switch string(in) {
	case "passed":
		return string(testkube.PASSED_ExecutionStatus)
	default:
		return string(testkube.FAILED_ExecutionStatus)
	}
}

// createSettingsXML saves the settings.xml to maven config folder and adds it to the list of arguments
func createSettingsXML(directory string, content string) (string, error) {
	settingsXML := filepath.Join(directory, "settings.xml")
	err := os.WriteFile(settingsXML, []byte(content), 0644)
	if err != nil {
		return "", fmt.Errorf("could not create settings.xml: %w", err)
	}

	return settingsXML, nil
}
