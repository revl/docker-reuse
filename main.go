// +build !windows

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var appName = "docker-reuse"

func findOrBuildAndPushImage(workingDir, imageName, templateFilename,
	dockerfile string, quiet bool) error {

	templateContents, err := ioutil.ReadFile(templateFilename)
	if err != nil {
		return err
	}

	// Image tag may contain lowercase and uppercase letters, digits,
	// underscores, periods and dashes.
	re := regexp.MustCompile(regexp.QuoteMeta(imageName) + "(?::[-.\\w]+)?")

	if len(re.Find(templateContents)) == 0 {
		return fmt.Errorf("'%s' does not contain references to '%s'",
			templateFilename, imageName)
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
	_, err = exec.Command(
		"docker", "manifest", "inspect", imageName).Output()

	if err == nil {
		if !quiet {
			fmt.Println("Image already exists")
		}
	} else {
		if ee, ok := err.(*exec.ExitError); ok {
			if !quiet {
				for _, l := range strings.Split(
					string(ee.Stderr[:]), "\n") {

					if l != "" {
						fmt.Fprintln(os.Stderr, l)
					}
				}
			}
		} else {
			return err
		}

		args := []string{"build", ".", "-t", imageName}
		if quiet {
			args = append(args, "-q")
		}
		if dockerfile != "" {
			args = append(args, "-f", dockerfile)
		}
		cmd := exec.Command("docker", args...)
		if !quiet {
			cmd.Stdout = os.Stdout
		}
		cmd.Stderr = os.Stderr
		if err = cmd.Run(); err != nil {
			return err
		}

		args = []string{"push", imageName}
		if quiet {
			args = append(args, "-q")
		}
		cmd = exec.Command("docker", args...)
		if !quiet {
			cmd.Stdout = os.Stdout
		}
		cmd.Stderr = os.Stderr
		if err = cmd.Run(); err != nil {
			return err
		}
	}

	templateContents = re.ReplaceAll(templateContents, []byte(imageName))

	return ioutil.WriteFile(templateFilename, templateContents, 0)
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

		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
