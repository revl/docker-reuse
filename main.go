//go:build !windows

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

func runDockerCmd(quiet bool, arg ...string) error {
	cmd := exec.Command("docker", arg...)
	cmd.Stderr = os.Stderr
	if !quiet {
		cmd.Stdout = os.Stdout
		fmt.Println("Run: docker", strings.Join(arg, " "))
	}
	return cmd.Run()
}

func findOrBuildAndPushImage(workingDir, imageName, templateFilename,
	placeholderString, dockerfile string,
	buildArgs []string, quiet bool) error {

	templateContents, err := os.ReadFile(templateFilename)
	if err != nil {
		return err
	}

	// Check if the placeholder is explicitly specified on the command line.
	placeholder := []byte(placeholderString)

	if len(placeholder) != 0 {
		if !bytes.Contains(templateContents, placeholder) {
			return fmt.Errorf(
				"'%s' does not contain occurrences of '%s'",
				templateFilename, placeholderString)
		}
	} else {
		// Use the image name itself as the placeholder.
		re := regexp.MustCompile(regexp.QuoteMeta(imageName) +
			// Image tag may contain lowercase and uppercase
			// letters, digits, underscores, periods, and dashes.
			"(?::[-.\\w]+)?")

		imageRefs := re.FindAll(templateContents, -1)

		if len(imageRefs) == 0 {
			return fmt.Errorf(
				"'%s' does not contain references to '%s'",
				templateFilename, imageName)
		}

		placeholder = imageRefs[0]

		// Check that all references to the image within the template
		// file are identical.
		for i := 1; i < len(imageRefs); i++ {
			if !bytes.Equal(imageRefs[i], placeholder) {
				return fmt.Errorf("'%s' contains "+
					"inconsistent references to '%s'",
					templateFilename, imageName)
			}
		}
	}

	fingerprint, err := computeFingerprint(
		workingDir, dockerfile, buildArgs, quiet)
	if err != nil {
		return err
	}

	imageName = imageName + ":" + fingerprint
	if !quiet {
		fmt.Println("Target image:", imageName)
	}

	// Check if the image already exists in the registry
	err = runDockerCmd(true, "manifest", "inspect", imageName)
	if err == nil {
		if !quiet {
			fmt.Println("Image already exists")
		}
	} else {
		// If the above command exited with a non-zero code, assume
		// that the image does not exist. Abort on all other errors.
		if _, ok := err.(*exec.ExitError); !ok {
			return err
		}

		// Build the image and push it to the container registry.

		args := []string{"build", ".", "-t", imageName}
		if quiet {
			args = append(args, "-q")
		}
		if dockerfile != "" {
			args = append(args, "-f", dockerfile)
		}
		for _, buildArg := range buildArgs {
			args = append(args, "--build-arg", buildArg)
		}
		if err = runDockerCmd(quiet, args...); err != nil {
			return err
		}

		args = []string{"push", imageName}
		if quiet {
			args = append(args, "-q")
		}
		if err = runDockerCmd(quiet, args...); err != nil {
			return err
		}
	}

	newImageRef := []byte(imageName)

	// No need to update the output file if it already contains
	// the right reference.
	if bytes.Equal(placeholder, newImageRef) {
		return nil
	}

	return os.WriteFile(templateFilename,
		bytes.ReplaceAll(templateContents, placeholder, newImageRef), 0)
}

var usage = `Usage:  docker-reuse [OPTIONS] PATH IMAGE FILE [ARG...]

Arguments:
  PATH
    	Docker build context directory
  IMAGE
    	Name of the image to find or build
  FILE
    	File to update with the new image tag
  [ARG...]
    	Optional build arguments (format: NAME[=value])

Options:`

func main() {
	var dockerfileFlag = flag.String("f", "",
		"Pathname of the `Dockerfile` (by default, 'PATH/Dockerfile')")

	var quietFlag = flag.Bool("q", false, "Suppress build output")

	var imagePlaceholderFlag = flag.String("p", "",
		"Placeholder for the image name in FILE "+
			"(by default, the image name itself)")

	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}

	flag.Parse()

	args := flag.Args()

	if len(args) < 3 {
		fmt.Fprintln(flag.CommandLine.Output(),
			"invalid number of positional arguments")
		flag.Usage()
		os.Exit(2)
	}

	buildArgs := args[3:]

	// Load any missing build argument values from the respective
	// environment variables.  This job cannot be left to docker
	// because argument values are part of the image fingerprint.
	for i, arg := range buildArgs {
		if !strings.ContainsRune(arg, '=') {
			buildArgs[i] = arg + "=" + os.Getenv(arg)
		}
	}

	if err := findOrBuildAndPushImage(args[0], args[1], args[2],
		*imagePlaceholderFlag, *dockerfileFlag,
		buildArgs, *quietFlag); err != nil {

		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
