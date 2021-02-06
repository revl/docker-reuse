// +build !windows

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var appName = "docker-reuse"

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
	dockerfile string, quiet bool) error {

	templateContents, err := ioutil.ReadFile(templateFilename)
	if err != nil {
		return err
	}

	// Image tag may contain lowercase and uppercase letters, digits,
	// underscores, periods and dashes.
	re := regexp.MustCompile(regexp.QuoteMeta(imageName) + "(?::[-.\\w]+)?")

	imageRefs := re.FindAll(templateContents, -1)

	if len(imageRefs) == 0 {
		return fmt.Errorf("'%s' does not contain references to '%s'",
			templateFilename, imageName)
	}

	originalImageRef := imageRefs[0]

	// Check that all references to the image within the template
	// file are identical.
	for i := 1; i < len(imageRefs); i++ {
		if bytes.Compare(imageRefs[i], originalImageRef) != 0 {
			return fmt.Errorf(
				"'%s' contains inconsistent references to '%s'",
				templateFilename, imageName)
		}
	}

	fingerprint, err := computeFingerprint(workingDir, dockerfile, quiet)
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
	if bytes.Compare(originalImageRef, newImageRef) == 0 {
		return nil
	}

	return ioutil.WriteFile(templateFilename,
		re.ReplaceAll(templateContents, newImageRef), 0)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(),
			"Usage:  "+appName+" build [OPTIONS] PATH IMAGE FILE\n"+
				"  PATH\n"+
				"    \tDocker build context directory\n"+
				"  IMAGE\n"+
				"    \tName of the image to find or build\n"+
				"  FILE\n"+
				"    \tFile to update with the new image tag")
		flag.PrintDefaults()
	}

	dockerfile := flag.String("f", "",
		"Pathname of the `Dockerfile` (Default is 'PATH/Dockerfile')")
	quiet := flag.Bool("q", false, "Suppress build output")

	flag.Parse()

	args := flag.Args()

	if len(args) != 3 {
		fmt.Fprintln(flag.CommandLine.Output(),
			"invalid number of positional arguments")
		flag.Usage()
		os.Exit(2)
	}

	if err := findOrBuildAndPushImage(
		args[0], args[1], args[2], *dockerfile, *quiet); err != nil {

		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
