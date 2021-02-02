// +build !windows

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var appName = "docker-reuse"

func findOrBuildAndPushImage(workingDir, imageName, templateFilename,
	dockerfile string, quiet bool) {

	templateContents, err := ioutil.ReadFile(templateFilename)
	if err != nil {
		log.Fatal(err)
	}

	// Image tag may contain lowercase and uppercase letters, digits,
	// underscores, periods and dashes.
	re := regexp.MustCompile(regexp.QuoteMeta(imageName) + "(?::[-.\\w]+)?")

	if len(re.Find(templateContents)) == 0 {
		log.Fatalf("'%s' does not contain references to '%s'",
			templateFilename, imageName)
	}

	fingerprint, err := computeFingerprint(workingDir, dockerfile, quiet)
	if err != nil {
		log.Fatal(err)
	}

	imageName = imageName + ":" + fingerprint
	if !quiet {
		log.Println(imageName)
	}

	// Check if the image already exists in the registry
	cmd := exec.Command("docker", "manifest", "inspect", imageName)
	// cmd.Stderr = nil
	_, err = cmd.Output()

	if err == nil {
		if !quiet {
			log.Print("Image already exists")
		}
	} else {
		if ee, ok := err.(*exec.ExitError); ok {
			for _, l := range strings.Split(
				string(ee.Stderr[:]), "\n") {

				if l != "" {
					log.Println(l)
				}
			}
		} else {
			log.Fatal(err)
		}
	}

	templateContents = re.ReplaceAll(templateContents, []byte(imageName))

	if err = ioutil.WriteFile(
		templateFilename, templateContents, 0); err != nil {

		log.Fatal(err)
	}
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

	findOrBuildAndPushImage(args[0], args[1], args[2], *dockerfile, *quiet)
}
