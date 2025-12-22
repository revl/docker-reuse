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

type templateFileStatus int

const (
	// The placeholder check has been performed in the template file.
	placeholderChecked templateFileStatus = iota + 1
	// The template file already contains the new image tag.
	contentsUnchanged
	// The placeholder has been replaced with the new image tag.
	placeholderReplaced
)

// templateFile represents a template file.
type templateFile struct {
	filename    string
	contents    []byte
	placeholder []byte
	status      templateFileStatus
}

// checkPlaceholder ensures that the placeholder for the new image tag exists
// in the file and is consistent across all occurrences in the template file
// if the placeholder is the image name itself.
func (tf *templateFile) checkPlaceholder(imageName string) error {
	// Return early if this template file was already checked via a
	// previous target file.
	if tf.status >= placeholderChecked {
		return nil
	}

	if len(tf.placeholder) == 0 {
		// Use the image name itself as the placeholder.
		// Image tag may contain lowercase and uppercase
		// letters, digits, underscores, periods, and dashes.
		re := regexp.MustCompile(
			regexp.QuoteMeta(imageName) + "(?::[-.\\w]+)?")

		imageRefs := re.FindAll(tf.contents, -1)

		if len(imageRefs) == 0 {
			return fmt.Errorf(
				"'%s' does not contain the image name '%s'",
				tf.filename, imageName)
		}

		tf.placeholder = imageRefs[0]

		// Check that all references to the image within the
		// template file are identical.
		for i := 1; i < len(imageRefs); i++ {
			if !bytes.Equal(imageRefs[i], tf.placeholder) {
				return fmt.Errorf("'%s' contains "+
					"inconsistent tags for '%s'",
					tf.filename, imageName)
			}
		}
	} else if !bytes.Contains(tf.contents, tf.placeholder) {
		return fmt.Errorf("'%s' does not contain occurrences of '%s'",
			tf.filename, tf.placeholder)
	}

	tf.status = placeholderChecked
	return nil
}

// replacePlaceholder replaces the placeholder with the new image tag in the
// template file and returns true if the file's contents were changed.
func (tf *templateFile) replacePlaceholder(fingerprintedImageName []byte) {
	if tf.status < contentsUnchanged {
		// Skip updating the template file if it already contains the
		// right reference.
		if bytes.Equal(tf.placeholder, fingerprintedImageName) {
			tf.status = contentsUnchanged
		} else {
			tf.contents = bytes.ReplaceAll(tf.contents,
				tf.placeholder, fingerprintedImageName)

			tf.status = placeholderReplaced
		}
	}
}

// targetFile represents a target file to overwrite with the template file's
// content where the placeholder is substituted with the new image tag.
type targetFile struct {
	filename string
	template *templateFile
}

func errorExit(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func fmtErrorExit(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", a...)
	os.Exit(1)
}

var placeholderOptionTag = new(struct{})
var templateOptionTag = new(struct{})
var writeToOptionTag = new(struct{})
var updateInPlaceOptionTag = new(struct{})

var dockerfileFlag string
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
			Description: "Path to the Dockerfile " +
				"(by default, 'PATH/Dockerfile')\n",
			Tag: &dockerfileFlag,
		},
		{
			Name:  "p|placeholder",
			Param: "STRING",
			Description: "Placeholder for the new image name " +
				"in the file specified by --template or " +
				"--update-in-place (by default, the image " +
				"name itself, including the tag)\n",
			Tag: &placeholderOptionTag,
		},
		{
			Name:  "template",
			Param: "FILE",
			Description: "Template file to use for the next " +
				"--write-to operation\n",
			Tag: &templateOptionTag,
		},
		{
			Name:  "write-to",
			Param: "FILE",
			Description: "File to overwrite with the template " +
				"file's content where the placeholder is " +
				"replaced with the new image tag. " +
				"Requires --template to be specified " +
				"earlier in the command line.\n",
			Tag: &writeToOptionTag,
		},
		{
			Name:  "u|update-in-place",
			Param: "FILE",
			Description: "File to read and update in place with " +
				"the new image tag (equivalent to --template " +
				"and --write-to pointing to the same file).\n",
			Tag: &updateInPlaceOptionTag,
		},
		{
			Name:  "t|tag",
			Param: "STRING",
			Description: "Additional tag to use for the image " +
				"(by default, only the 160-bit fingerprint " +
				"computed from the image sources is used)\n",
			Tag: &additionalTags,
		},
		{
			Name:  "m|mode",
			Param: "MODE",
			Description: "Fingerprinting mode: " +
				fingerprintModeOptions() + "\n",
			Tag: &modeFlag,
		},
		{
			Name:  "platform",
			Param: "PLATFORM",
			Description: "Target platform for the image " +
				"(e.g., linux/amd64)\n",
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

	var placeholder []byte
	var placeholderOptionToken string
	var lastTemplateFile *templateFile
	var targetFiles []targetFile

	newTemplateFile := func(filename string) *templateFile {
		contents, err := os.ReadFile(filename)
		if err != nil {
			fmtErrorExit("could not read template file: %v", err)
		}
		return &templateFile{
			filename:    filename,
			contents:    contents,
			placeholder: placeholder,
		}
	}

	addTargetFile := func(filename string, template *templateFile) {
		targetFiles = append(targetFiles, targetFile{
			filename: filename,
			template: template,
		})
	}

	for _, opt := range parseResult.OptsAndArgs {
		switch opt.Tag {
		case &placeholderOptionTag:
			placeholder = []byte(opt.Value)
			placeholderOptionToken = opt.Token
		case &templateOptionTag:
			lastTemplateFile = newTemplateFile(opt.Value)
		case &writeToOptionTag:
			if lastTemplateFile == nil {
				parseResult.HandleError(fmt.Errorf(
					"%s requires --template to be defined",
					opt.Token))
			}
			addTargetFile(opt.Value, lastTemplateFile)
		case &updateInPlaceOptionTag:
			addTargetFile(opt.Value, newTemplateFile(opt.Value))
		default:
			switch opt.Tag.(type) {
			case *string:
				*opt.Tag.(*string) = opt.Value
			case *[]string:
				*opt.Tag.(*[]string) =
					append(*opt.Tag.(*[]string), opt.Value)
			case *bool:
				*opt.Tag.(*bool) = true
			}
		}
	}

	if placeholder != nil && lastTemplateFile == nil {
		parseResult.HandleError(fmt.Errorf("%s has no effect "+
			"without --template or --update-in-place",
			placeholderOptionToken))
	}
	if lastTemplateFile != nil && len(targetFiles) == 0 {
		parseResult.HandleError(fmt.Errorf(
			"--template has no effect without --write-to"))
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

	// Check that the placeholders actually exist in their template files
	// before building the image. Use the image name as the placeholder
	// if no placeholder was specified.
	for _, tf := range targetFiles {
		if err := tf.template.checkPlaceholder(imageName); err != nil {
			errorExit(err)
		}
	}

	// Find or build the image and get its fingerprint tag.
	fingerprintedImageName, err := findOrBuildAndPushImage(
		workingDir, imageName, buildArgs, dockerfileFlag,
		additionalTags, computeFingerprint, platformFlag, quietFlag)
	if err != nil {
		errorExit(err)
	}

	for _, tf := range targetFiles {
		tf.template.replacePlaceholder([]byte(fingerprintedImageName))

		// Skip unchanged update-in-place files.
		if tf.template.status == contentsUnchanged &&
			tf.template.filename == tf.filename {
			continue
		}

		// Replace the placeholder with the fingerprinted image name.
		if err = os.WriteFile(tf.filename,
			tf.template.contents, 0600); err != nil {
			fmtErrorExit("could not overwrite %s: %v",
				tf.filename, err)
		}
	}
}
