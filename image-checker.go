package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func getExitCode(err error) (int, error) {
	exitcode := 0
	if exiterr, ok := err.(*exec.ExitError); ok {
		if procExit := exiterr.Sys().(syscall.WaitStatus); ok {
			return procExit.ExitStatus(), nil
		}
	}
	return exitcode, fmt.Errorf("failed to get the exit code")
}

func processExitCode(err error) (exitCode int) {
	if err != nil {
		var exiterr error
		if exitCode, exiterr = getExitCode(err); exiterr != nil {
			// TODO: Fix this so we check the error's text.
			// we've failed to retrieve exit code, so we set it to 127
			exitCode = 127
		}
	}
	return
}

func runCommandWithOutput(cmd *exec.Cmd) (output string, exitCode int, err error) {
	exitCode = 0
	out, err := cmd.CombinedOutput()
	exitCode = processExitCode(err)
	output = string(out)
	return
}

func runCommand(cmd *exec.Cmd) (exitCode int, err error) {
	exitCode = 0
	err = cmd.Run()
	exitCode = processExitCode(err)
	return
}

func printUsage() {
	fmt.Println("USAGE: image-checker [options] image")
	flag.VisitAll(func(fl *flag.Flag) {
		fmt.Printf("  -%s=\"%s\" %s\n", fl.Name, fl.DefValue, fl.Usage)
	})
}

func dockerCmd(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	out, exitCode, err := runCommandWithOutput(cmd)
	if err != nil || exitCode != 0 {
		return "", fmt.Errorf("failed to run command: %s", err)
	}
	return out, nil
}

func pullImage(image string) error {
	_, err := dockerCmd("pull", image)
	if err != nil {
		err = fmt.Errorf("encountered error while pulling '%s': %s", image, err)
	}
	return err
}

func imageExists(image string) error {
	_, err := dockerCmd("inspect", image)
	if err != nil {
		err = fmt.Errorf("'%s' doesn't exist: %s", image, err)
	}
	return err
}

func runContainer(args []string) (string, error) {
	out, err := dockerCmd(args...)
	if err != nil {
		return "", fmt.Errorf("failed to run the container: %s", err)
	}
	containerID := strings.Trim(out, "\n")
	return containerID, nil
}

func deleteContainer(containerID string) error {
	_, err := dockerCmd("rm", containerID)
	if err != nil {
		return fmt.Errorf("failed to delete the container: %s", err)
	}
	return err
}

func stopContainer(containerID string) error {
	_, err := dockerCmd("kill", "-s", "TERM", containerID)
	if err != nil {
		err = fmt.Errorf("failed to stop the container: %s", err)
	}
	return err
}

func startContainer(containerID string) error {
	_, err := dockerCmd("start", containerID)
	if err != nil {
		return fmt.Errorf("failed to start the container: %s", err)
	}
	return err
}

func killContainer(containerID string) error {
	dockerRun := exec.Command("docker", "kill", containerID)
	exitCode, err := runCommand(dockerRun)

	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to kill the container: %s", err)
	}
	return err
}

func checkErr(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func getContainerState(id string) (int, bool, error) {
	var (
		exitStatus int
		running    bool
	)
	out, err := dockerCmd("inspect", "--format={{.State.Running}} {{.State.ExitCode}}", id)
	if err != nil {
		return 0, false, fmt.Errorf("'%s' doesn't exist: %s", id, err)
	}

	out = strings.Trim(out, "\n")
	splitOutput := strings.Split(out, " ")
	if len(splitOutput) != 2 {
		return 0, false, fmt.Errorf("failed to get container state: output is broken")
	}
	if splitOutput[0] == "true" {
		running = true
	}
	if n, err := strconv.Atoi(splitOutput[1]); err == nil {
		exitStatus = n
	} else {
		return 0, false, fmt.Errorf("failed to get container state: couldn't parse integer")
	}

	return exitStatus, running, nil
}

func cleanUp(id string) {
	killContainer(id)
	deleteContainer(id)
}

func printResults(image string, canStop, canRestart, needsArgs bool) {
	var (
		stop    = "FAILED"
		restart = "FAILED"
		args    = "YES"
	)

	if canStop {
		stop = "PASSED"
	}

	if canRestart {
		restart = "PASSED"
	}

	if !needsArgs {
		args = "NO"
	}

	fmt.Printf("image-checker results for '%s':\n", image)
	fmt.Printf("Test graceful stop: %s\n", stop)
	fmt.Printf("Test start after stopping: %s\n", restart)
	fmt.Printf("Command was specified: %s\n", args)
}

var errTimeout = fmt.Errorf("timed out while running command")

func timeoutDockerCmd(timeout int, args ...string) (err error) {
	done := make(chan error)
	go func() {
		_, err := dockerCmd(args...)
		if err != nil {
			done <- err
		}
		done <- nil
	}()
	select {
	case <-time.After(time.Duration(timeout) * time.Second):
		fmt.Println("timeout reached")
		return errTimeout
	case err = <-done:
		return err
	}
}

func main() {
	var (
		flRunArgs          = flag.String("runargs", "-d", "necessary options for running the container")
		flRunCmd           = flag.String("runcmd", "", "command to run in the container; defaults to entrypoint/cmd")
		flCleanupOnFailure = flag.Bool("autocleanup", true, "remove the container when running into errors")
		args               = []string{"run"}
		handlesStop        = true
		handlesRestart     = true
		commandSpecified   = false
	)
	flag.Parse()

	image := flag.Arg(0)
	if image == "" {
		fmt.Println("ERROR: the image must be specified")
		printUsage()
		os.Exit(1)
	}

	if *flRunArgs == "" {
		fmt.Println("the runargs can't be empty. At least '-d' should be provided")
	}

	if err := imageExists(image); err != nil {
		err := pullImage(image)
		checkErr(err)
	}

	runArgs := strings.Split(*flRunArgs, " ")
	args = append(args, runArgs...)
	args = append(args, image)
	if len(*flRunCmd) > 0 {
		commandSpecified = true
		runCmd := strings.Split(*flRunCmd, " ")
		args = append(args, runCmd...)
	}

	containerID, err := runContainer(args)
	checkErr(err)
	time.Sleep(5 * time.Second)

	exitCode, isRunning, err := getContainerState(containerID)
	if err != nil {
		err = fmt.Errorf("failed to get the container state: %s", err)
	}

	if !isRunning && err == nil {
		err = fmt.Errorf("failure: container %s exited with %d", containerID, exitCode)
	}
	if err != nil {
		if *flCleanupOnFailure {
			cleanUp(containerID)
		}
		fmt.Println(err)
		os.Exit(1)
	}

	// try to stop the container with SIGTERM
	err = stopContainer(containerID)
	if err != nil {
		fmt.Printf("failed to stop container %s: %s\n", containerID, err)
		if *flCleanupOnFailure {
			cleanUp(containerID)
		}
		os.Exit(1)
	}

	err = timeoutDockerCmd(5, "wait", containerID)
	if err == errTimeout {
		handlesStop = false
	}

	exitCode, isRunning, err = getContainerState(containerID)
	if err != nil {
		fmt.Printf("failed to get the container state: %s\n", err)
		os.Exit(1)
	}

	if exitCode != 0 || isRunning {
		handlesStop = false
		killContainer(containerID)
	}

	// try to start the container again
	err = startContainer(containerID)
	if err != nil || !handlesStop {
		handlesRestart = false
	}

	_, isRunning, err = getContainerState(containerID)
	if err != nil {
		fmt.Printf("failed to get the container state: %s\n", err)
		os.Exit(1)
	}

	if !isRunning {
		handlesRestart = false
	}

	if !handlesStop || !handlesRestart {
		if *flCleanupOnFailure {
			cleanUp(containerID)
		}
		printResults(image, handlesStop, handlesRestart, commandSpecified)
		os.Exit(2)
	}

	// try to stop the container with SIGTERM again
	err = stopContainer(containerID)
	if err != nil {
		fmt.Printf("failed to stop container after restart: %s\n", err)
		handlesStop = false
		killContainer(containerID)
	}

	err = timeoutDockerCmd(5, "wait", containerID)
	if err == errTimeout {
		handlesStop = false
	}
	exitCode, isRunning, err = getContainerState(containerID)
	if err != nil {
		fmt.Printf("failed to get the container state: %s\n", err)
		os.Exit(1)
	}
	if exitCode != 0 || isRunning {
		handlesStop = false
		killContainer(containerID)
	}

	printResults(image, handlesStop, handlesRestart, commandSpecified)
	cleanUp(containerID)
}
