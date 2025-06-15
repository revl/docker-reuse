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

// stringSliceFlag is a custom flag type that allows multiple values
type stringSliceFlag []string

func (f *stringSliceFlag) String() string {
	return strings.Join(*f, ", ")
}

func (f *stringSliceFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// runDockerCmd runs a docker command and prints the command to the standard
// output if not quiet.
func runDockerCmd(quiet bool, arg ...string) error {
	cmd := exec.Command("docker", arg...)
	cmd.Stderr = os.Stderr
	if !quiet {
		cmd.Stdout = os.Stdout
		fmt.Println("Run: docker", strings.Join(arg, " "))
	}
	return cmd.Run()
}

// findOrBuildAndPushImage finds an existing image or builds and pushes a new
// image to the container registry.
func findOrBuildAndPushImage(workingDir, imageName string, buildArgs []string,
	dockerfile string, additionalTags []string,
	computeFingerprint fingerprintFunc, quiet bool) (string, error) {

	fingerprint, err := computeImageFingerprint(
		workingDir, dockerfile, buildArgs, computeFingerprint, quiet)
	if err != nil {
		return "", err
	}

	imageNameWithFingerprint := imageName + ":" + fingerprint.hash
	if !quiet {
		fmt.Println("Fingerprinted image:", imageNameWithFingerprint)
	}

	var imagesToPush []string

	// Check if the image with the fingerprint already exists
	// in the registry.
	if err = runDockerCmd(true, "manifest", "inspect",
		imageNameWithFingerprint); err == nil {

		if !quiet {
			fmt.Println("Image already exists")
		}

		// Tag the image with the additional tags.
		for _, tag := range additionalTags {
			imageNameWithTag := imageName + ":" + tag
			if err = runDockerCmd(quiet, "tag",
				imageNameWithFingerprint,
				imageNameWithTag); err != nil {
				return "", err
			}
			imagesToPush = append(imagesToPush, imageNameWithTag)
		}
	} else {
		// If the manifest inspect command exited with a non-zero code,
		// assume that the image does not exist.  Abort on all other
		// errors.
		if _, ok := err.(*exec.ExitError); !ok {
			return "", err
		}

		// Build the image.
		args := []string{"build", ".", "-t", imageNameWithFingerprint}
		imagesToPush = []string{imageNameWithFingerprint}
		for _, tag := range additionalTags {
			imageNameWithTag := imageName + ":" + tag
			args = append(args, "-t", imageNameWithTag)
			imagesToPush = append(imagesToPush, imageNameWithTag)
		}
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
			return "", err
		}
	}

	// Push the images to the container registry.
	for _, imageNameWithTag := range imagesToPush {
		args := []string{"push", imageNameWithTag}
		if quiet {
			args = append(args, "-q")
		}
		if err = runDockerCmd(quiet, args...); err != nil {
			return "", err
		}
	}

	return imageNameWithFingerprint, nil
}

var usage = `Usage:  docker-reuse [OPTIONS] PATH IMAGE [ARG...]

Arguments:
  PATH
    	Docker build context directory
  IMAGE
    	Name of the image to find or build
  [ARG...]
    	Optional build arguments (format: NAME[=value])

Options:`

func usageError(message string) {
	fmt.Fprintln(flag.CommandLine.Output(), "Error: "+message)
	flag.Usage()
	os.Exit(2)
}

func errorExit(message string, a ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+message+"\n", a...)
	os.Exit(1)
}

func main() {
	var dockerfileFlag = flag.String("f", "",
		"Pathname of the Dockerfile (by default, 'PATH/Dockerfile')")

	var templateFilenameFlag = flag.String("u", "",
		"Template file to update with the new image tag")

	var imagePlaceholderFlag = flag.String("p", "",
		"Placeholder for the image name in template file "+
			"(by default, the image name itself)")

	var additionalTags stringSliceFlag
	flag.Var(&additionalTags, "t",
		"Additional tag(s) to apply to the image")

	var modeFlag = flag.String("m", string(modeAuto),
		"Fingerprinting mode: "+fingerprintModeOptions())

	var quietFlag = flag.Bool("q", false, "Suppress build output")

	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}

	flag.Parse()

	if *imagePlaceholderFlag != "" && *templateFilenameFlag == "" {
		usageError("-p requires -u")
	}

	var computeFingerprint fingerprintFunc

	switch fingerprintMode(*modeFlag) {
	case modeCommit:
		computeFingerprint = getLastCommitHash
	case modeSHA1:
		computeFingerprint = hashFiles
	case modeAuto:
		computeFingerprint = func(
			pathname string) (fingerprint, error) {

			fp, err := getLastCommitHash(pathname)
			if err == nil {
				return fp, nil
			}

			fmt.Fprintf(os.Stderr, "Warning: unable to use git "+
				"commit hash for '%s' - falling back to "+
				"file content hashing: %v\n", pathname, err)

			return hashFiles(pathname)
		}
	default:
		errorExit("invalid mode: %s; allowed values: %s",
			*modeFlag, fingerprintModeOptions())
	}

	args := flag.Args()

	if len(args) < 2 {
		usageError("invalid number of positional arguments")
	}

	workingDir, imageName, buildArgs := args[0], args[1], args[2:]

	// Load any missing build argument values from the respective
	// environment variables.  This job cannot be left to docker
	// because argument values are part of the image fingerprint.
	for i, arg := range buildArgs {
		if !strings.ContainsRune(arg, '=') {
			envValue, found := os.LookupEnv(arg)
			if !found {
				errorExit("environment variable %s is not set",
					arg)
			}
			buildArgs[i] = arg + "=" + envValue
		}
	}

	if *templateFilenameFlag == "" {
		if _, err := findOrBuildAndPushImage(
			workingDir, imageName, buildArgs, *dockerfileFlag,
			additionalTags, computeFingerprint,
			*quietFlag); err != nil {
			errorExit("%v", err)
		}
		return
	}

	templateContents, err := os.ReadFile(*templateFilenameFlag)
	if err != nil {
		errorExit("failed to read template file %s: %v",
			*templateFilenameFlag, err)
	}

	// Check if the placeholder is explicitly specified on the
	// command line.
	placeholder := []byte(*imagePlaceholderFlag)

	if len(placeholder) == 0 {
		// Use the image name itself as the placeholder.
		// Image tag may contain lowercase and uppercase
		// letters, digits, underscores, periods, and dashes.
		re := regexp.MustCompile(regexp.QuoteMeta(imageName) +
			"(?::[-.\\w]+)?")

		imageRefs := re.FindAll(templateContents, -1)

		if len(imageRefs) == 0 {
			errorExit("'%s' does not contain references to '%s'",
				*templateFilenameFlag, imageName)
		}

		placeholder = imageRefs[0]

		// Check that all references to the image within the
		// template file are identical.
		for i := 1; i < len(imageRefs); i++ {
			if !bytes.Equal(imageRefs[i], placeholder) {
				errorExit("'%s' contains inconsistent "+
					"references to '%s'",
					*templateFilenameFlag,
					imageName)
			}
		}
	} else if !bytes.Contains(templateContents, placeholder) {
		errorExit("'%s' does not contain occurrences of '%s'",
			*templateFilenameFlag, *imagePlaceholderFlag)
	}

	// Find or build the image and get its fingerprint tag.
	imageNameWithFingerprint, err := findOrBuildAndPushImage(
		workingDir, imageName, buildArgs, *dockerfileFlag,
		additionalTags, computeFingerprint, *quietFlag)
	if err != nil {
		errorExit("%v", err)
	}

	// Skip updating the template file if it already contains
	// the right reference.
	if *imagePlaceholderFlag != imageNameWithFingerprint {
		newTemplateContents := bytes.ReplaceAll(
			templateContents,
			placeholder, []byte(imageNameWithFingerprint))
		if err := os.WriteFile(*templateFilenameFlag,
			newTemplateContents, 0); err != nil {
			errorExit("could not overwrite %s: %v",
				*templateFilenameFlag, err)
		}
	}
}
