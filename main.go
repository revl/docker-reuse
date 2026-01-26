//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"

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

// checkImageExists checks if the image with the fingerprint already exists.
// It skips checking the registry if checkLocalCache is true.
func checkImageExists(imageNameWithFingerprint string,
	checkLocalCache bool) (bool, error) {
	if checkLocalCache {
		err := runDockerCmd(true, "image", "inspect", "-f", "{{.Id}}",
			imageNameWithFingerprint)
		if err == nil {
			return true, nil
		}
		if _, ok := err.(*exec.ExitError); !ok {
			return false, err
		}
	}

	err := runDockerCmd(true, "manifest", "inspect",
		imageNameWithFingerprint)
	if err == nil {
		return true, nil
	}
	if _, ok := err.(*exec.ExitError); !ok {
		return false, err
	}

	return false, nil
}

// findOrBuildAndPushImage finds an existing image or builds and pushes a new
// image to the container registry.
func findOrBuildAndPushImage(workingDir, imageName string, buildArgs []string,
	dockerfile string, additionalTags []string,
	computeFingerprint fingerprintFunc, platform string,
	checkLocalCache, quiet bool) (string, error) {

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

	imageExists, err := checkImageExists(
		imageNameWithFingerprint, checkLocalCache)
	if err != nil {
		return "", err
	}

	if imageExists {
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
	placeholder []byte
}

// determinePlaceholder finds the placeholder for the new image tag in the
// template file, and when the placeholder is the image name itself, ensures
// that all occurrences of that image name are identical.
func (tf *templateFile) determinePlaceholder(contents []byte,
	imageName string) error {
	if len(tf.placeholder) == 0 {
		// Use the image name itself as the placeholder.
		// Image tag may contain lowercase and uppercase
		// letters, digits, underscores, periods, and dashes.
		re := regexp.MustCompile(
			regexp.QuoteMeta(imageName) + "(?::[-.\\w]+)?")

		imageRefs := re.FindAll(contents, -1)

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
	} else if !bytes.Contains(contents, tf.placeholder) {
		return fmt.Errorf("'%s' does not contain occurrences of '%s'",
			tf.filename, tf.placeholder)
	}

	return nil
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
var checkLocalCacheFlag bool
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
			Name: "check-local-cache",
			Description: "Skip pushing the image to the registry " +
				"if it exists in the local cache.\n",
			Tag: &checkLocalCacheFlag,
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
		return &templateFile{
			filename:    filename,
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
		contents, err := os.ReadFile(tf.template.filename)
		if err != nil {
			fmtErrorExit("could not read template file: %v", err)
		}
		if err := tf.template.determinePlaceholder(contents,
			imageName); err != nil {
			errorExit(err)
		}
	}

	// Find or build the image and get its fingerprint tag.
	fingerprintedImageName, err := findOrBuildAndPushImage(
		workingDir, imageName, buildArgs, dockerfileFlag,
		additionalTags, computeFingerprint, platformFlag,
		checkLocalCacheFlag, quietFlag)
	if err != nil {
		errorExit(err)
	}

	for _, tf := range targetFiles {
		templateFilename := tf.template.filename
		targetFilename := tf.filename

		// Open template file for reading.
		templateFile, err := os.OpenFile(templateFilename, os.O_RDONLY, 0)
		if err != nil {
			fmtErrorExit("could not open %s: %v",
				templateFilename, err)
		}
		defer templateFile.Close()

		// Acquire an exclusive lock on the template file before
		// reading it to prevent concurrent updates.
		if err = syscall.Flock(int(templateFile.Fd()),
			syscall.LOCK_EX); err != nil {
			fmtErrorExit("could not lock template file %s: %v",
				templateFilename, err)
		}

		// Read the template file contents again (in case
		// it was changed while the image was being built).
		contents, err := io.ReadAll(templateFile)
		if err != nil {
			fmtErrorExit("could not read %s: %v",
				templateFilename, err)
		}

		if err := tf.template.determinePlaceholder(contents,
			imageName); err != nil {
			errorExit(err)
		}

		// Check if template and target are the same file by comparing
		// inodes.
		var stat syscall.Stat_t
		if err = syscall.Stat(templateFilename, &stat); err != nil {
			fmtErrorExit("could not stat template file %s: %v",
				templateFilename, err)
		}
		templateInode := stat.Ino
		targetInode := uint64(0)
		if err = syscall.Stat(targetFilename, &stat); err == nil {
			targetInode = stat.Ino
		} else if !os.IsNotExist(err) {
			fmtErrorExit("could not stat target file %s: %v",
				targetFilename, err)
		}

		// Replace the placeholder with the new image tag unless it is
		// already there.
		if !bytes.Equal(tf.template.placeholder,
			[]byte(fingerprintedImageName)) {
			contents = bytes.ReplaceAll(contents,
				tf.template.placeholder,
				[]byte(fingerprintedImageName))
		} else if templateInode == targetInode {
			// Skip updating the target file if it is the same file
			// as the template file and already contains the right
			// image tag.
			continue
		}

		// Open target file for writing while holding the exclusive lock
		// on the template file. If the template and target are the same
		// file, the lock on the target file will not be acquired.
		targetFile, err := os.OpenFile(targetFilename,
			os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			fmtErrorExit("could not open target file %s: %v",
				targetFilename, err)
		}
		defer targetFile.Close()

		// Acquire an exclusive lock on the target file unless it is
		// the same file as the template file.
		if templateInode != targetInode {
			if err = syscall.Flock(int(targetFile.Fd()),
				syscall.LOCK_EX); err != nil {
				fmtErrorExit("could not lock file %s: %v",
					targetFilename, err)
			}
		}

		// Write the processed template to the target file.
		if _, err := targetFile.Write(contents); err != nil {
			fmtErrorExit("could not write to target file %s: %v",
				targetFilename, err)
		}
		if err := targetFile.Truncate(int64(len(contents))); err != nil {
			fmtErrorExit("could not truncate target file %s: %v",
				targetFilename, err)
		}
		if err := targetFile.Sync(); err != nil {
			fmtErrorExit("could not sync target file %s: %v",
				targetFilename, err)
		}
	}
}
