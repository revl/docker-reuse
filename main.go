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

	// Check if the image with the fingerprint already exists
	// in the registry.
	if err = runDockerCmd(true, "manifest", "inspect",
		imageNameWithFingerprint); err == nil {

		if !quiet {
			fmt.Println("Image already exists")
		}

		// Tag the image with the additional tags.
		if len(additionalTags) > 0 {
			args := []string{"buildx", "imagetools", "create",
				imageNameWithFingerprint}
			for _, tag := range additionalTags {
				args = append(args, "--tag", imageName+":"+tag)
			}
			if err = runDockerCmd(quiet, args...); err != nil {
				return "", fmt.Errorf(
					"failed to tag the image: %v", err)
			}
		}
	} else {
		var imagesToPush []string

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
			return "", fmt.Errorf(
				"failed to build the image: %v", err)
		}

		// Push the images to the container registry.
		for _, imageNameWithTag := range imagesToPush {
			args := []string{"push", imageNameWithTag}
			if quiet {
				args = append(args, "-q")
			}
			if err = runDockerCmd(quiet, args...); err != nil {
				return "", fmt.Errorf(
					"failed to push the image: %v", err)
			}
		}
	}

	return imageNameWithFingerprint, nil
}

// templateFile represents a template file.
type templateFile struct {
	filename    string
	contents    []byte
	placeholder []byte
}

// readTemplateFile reads the template file and returns its contents and the
// placeholder for the fingerprinted image name.
func readTemplateFile(filename, imageName, placeholderString string) (
	templateFile, error) {

	result := templateFile{
		filename:    filename,
		placeholder: []byte(placeholderString),
	}
	var err error

	result.contents, err = os.ReadFile(filename)
	if err != nil {
		return templateFile{}, fmt.Errorf(
			"failed to read template file %s: %v", filename, err)
	}

	if len(result.placeholder) == 0 {
		// Use the image name itself as the placeholder.
		// Image tag may contain lowercase and uppercase
		// letters, digits, underscores, periods, and dashes.
		re := regexp.MustCompile(
			regexp.QuoteMeta(imageName) + "(?::[-.\\w]+)?")

		imageRefs := re.FindAll(result.contents, -1)

		if len(imageRefs) == 0 {
			return templateFile{}, fmt.Errorf(
				"'%s' does not contain the image name '%s'",
				filename, imageName)
		}

		result.placeholder = imageRefs[0]

		// Check that all references to the image within the
		// template file are identical.
		for i := 1; i < len(imageRefs); i++ {
			if !bytes.Equal(imageRefs[i], result.placeholder) {
				return templateFile{}, fmt.Errorf(
					"'%s' contains different tags for '%s'",
					filename, imageName)
			}
		}
	} else if !bytes.Contains(result.contents, result.placeholder) {
		return templateFile{}, fmt.Errorf(
			"'%s' does not contain occurrences of '%s'",
			filename, placeholderString)
	}

	return result, nil
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

func errorExit(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func fmtErrorExit(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	var dockerfileFlag = flag.String("f", "",
		"Pathname of the Dockerfile (by default, 'PATH/Dockerfile')")

	var templateFilenames stringSliceFlag
	flag.Var(&templateFilenames, "u",
		"Template file(s) to update with the new image tag")

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

	if *imagePlaceholderFlag != "" && len(templateFilenames) == 0 {
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
		fmtErrorExit("invalid mode: %s; allowed values: %s",
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
				fmtErrorExit(
					"environment variable %s is not set",
					arg)
			}
			buildArgs[i] = arg + "=" + envValue
		}
	}

	if len(templateFilenames) == 0 {
		if _, err := findOrBuildAndPushImage(
			workingDir, imageName, buildArgs, *dockerfileFlag,
			additionalTags, computeFingerprint,
			*quietFlag); err != nil {
			errorExit(err)
		}
		return
	}

	// Read all template files.
	var templateFiles []templateFile

	for _, fn := range templateFilenames {
		tf, err := readTemplateFile(
			fn, imageName, *imagePlaceholderFlag)
		if err != nil {
			errorExit(err)
		}
		templateFiles = append(templateFiles, tf)
	}

	// Find or build the image and get its fingerprint tag.
	fingerprintedImageName, err := findOrBuildAndPushImage(
		workingDir, imageName, buildArgs, *dockerfileFlag,
		additionalTags, computeFingerprint, *quietFlag)
	if err != nil {
		errorExit(err)
	}

	for _, tf := range templateFiles {
		// Skip updating the template file if it already contains
		// the right reference.
		if bytes.Equal(tf.placeholder, []byte(fingerprintedImageName)) {
			continue
		}

		// Replace the placeholder with the fingerprinted image name.
		if err = os.WriteFile(tf.filename,
			bytes.ReplaceAll(tf.contents, tf.placeholder,
				[]byte(fingerprintedImageName)),
			0); err != nil {
			fmtErrorExit("could not overwrite %s: %v",
				tf.filename, err)
		}
	}
}
