//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/revl/verbs"
)

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
	computeFingerprint fingerprintFunc, platform string,
	quiet bool) (string, error) {

	fingerprint, err := computeImageFingerprint(
		workingDir, dockerfile, buildArgs, computeFingerprint,
		platform, quiet)
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
		args := []string{"build", workingDir,
			"-t", imageNameWithFingerprint}
		imagesToPush = []string{imageNameWithFingerprint}
		for _, tag := range additionalTags {
			imageNameWithTag := imageName + ":" + tag
			args = append(args, "-t", imageNameWithTag)
			imagesToPush = append(imagesToPush, imageNameWithTag)
		}
		if platform != "" {
			args = append(args, "--platform", platform)
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

func errorExit(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func fmtErrorExit(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", a...)
	os.Exit(1)
}

var dockerfileFlag string
var templateFilenames []string
var imagePlaceholderFlag string
var additionalTags []string
var modeFlag string
var platformFlag string
var quietFlag bool
var workingDir string
var imageName string
var buildArgs []string

var cli = &verbs.CLI{
	Summary: "Find or build and push Docker images with fingerprinting",
	Options: []*verbs.Option{
		{
			Name:  "f|dockerfile",
			Param: "FILE",
			Description: "Path to the Dockerfile" +
				"(by default, 'PATH/Dockerfile')",
			Tag: &dockerfileFlag,
		},
		{
			Name:  "u|update-in-place",
			Param: "FILE",
			Description: "Template file(s) to update with " +
				"the new image tag",
			Tag: &templateFilenames,
		},
		{
			Name:  "p|placeholder",
			Param: "STRING",
			Description: "Placeholder for the image " +
				"name in the file specified by -u " +
				"(by default, the image name itself)",
			Tag: &imagePlaceholderFlag,
		},
		{
			Name:  "t|tag",
			Param: "STRING",
			Description: "Additional tag to use for the image" +
				"(by default, only the 160-bit fingerprint " +
				"computed from the image sources is used)",
			Tag: &additionalTags,
		},
		{
			Name:  "m|mode",
			Param: "MODE",
			Description: "Fingerprinting mode: " +
				fingerprintModeOptions(),
			Tag: &modeFlag,
		},
		{
			Name:  "platform",
			Param: "PLATFORM",
			Description: "Target platform for the image " +
				"(e.g., linux/amd64)",
			Tag: &platformFlag,
		},
		{
			Name:        "q|quiet",
			Description: "Suppress build output",
			Tag:         &quietFlag,
		},
	},
	Args: []*verbs.Arg{
		{
			Name:        "PATH",
			Description: "Docker build context directory",
			Tag:         &workingDir,
		},
		{
			Name:        "IMAGE",
			Description: "Name of the image to find or build",
			Tag:         &imageName,
		},
		{
			Name: "ARG",
			Description: "Optional build arguments " +
				"(format: NAME[=value])",
			Occurrence: verbs.ZeroOrMore,
			Tag:        &buildArgs,
		},
	},
}

func main() {
	parseResult := verbs.NewParser(cli).Parse(os.Args)

	var placeholderOptionToken string

	for _, opt := range parseResult.OptsAndArgs {
		switch opt.Tag.(type) {
		case *string:
			*opt.Tag.(*string) = opt.Value
		case *[]string:
			*opt.Tag.(*[]string) =
				append(*opt.Tag.(*[]string), opt.Value)
		case *bool:
			*opt.Tag.(*bool) = true
		}

		if opt.Tag == &imagePlaceholderFlag {
			placeholderOptionToken = opt.Token
		}
	}

	if imagePlaceholderFlag != "" && len(templateFilenames) == 0 {
		parseResult.HandleError(fmt.Errorf("%s requires -u",
			placeholderOptionToken))
	}

	var computeFingerprint fingerprintFunc

	switch fingerprintMode(modeFlag) {
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
		parseResult.HandleError(fmt.Errorf(
			"invalid mode: %s; allowed values: %s",
			modeFlag, fingerprintModeOptions()))
	}

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
			workingDir, imageName, buildArgs, dockerfileFlag,
			additionalTags, computeFingerprint,
			platformFlag, quietFlag); err != nil {
			errorExit(err)
		}
		return
	}

	// Read all template files.
	var templateFiles []templateFile

	for _, fn := range templateFilenames {
		tf, err := readTemplateFile(
			fn, imageName, imagePlaceholderFlag)
		if err != nil {
			errorExit(err)
		}
		templateFiles = append(templateFiles, tf)
	}

	// Find or build the image and get its fingerprint tag.
	fingerprintedImageName, err := findOrBuildAndPushImage(
		workingDir, imageName, buildArgs, dockerfileFlag,
		additionalTags, computeFingerprint, platformFlag, quietFlag)
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
