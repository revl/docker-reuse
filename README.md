# docker-reuse

## Overview

`docker-reuse` is a tool for building and publishing Docker images. It has two
related and complementary purposes:

1. Make the image content-addressable by tagging it with a fingerprint
   computed from the sources referenced in the Dockerfile.

   As a result, the image tag never changes unless the sources have changed,
   and so Kubernetes (or another orchestration system) won't have to restart
   the containers that use that image.

2. Save time and resources by bypassing the lengthy build and push operations
   if not a single source has changed.

   This performance improvement is less significant for the local Docker
   builds, which are relatively fast if the sources did not change. However,
   for the environments where Docker layer caching is not available (like
   Google Cloud Build), knowing when it is safe to skip the entire build can
   be a huge time-saver.

Here's how `docker-reuse` works:

1. It computes a 160-bit fingerprint from the Dockerfile sources.
2. It attempts to find a previously built image in the registry using the
   fingerprint as a tag.
3. If no such image exists, the tool builds it and pushes it to the registry.
4. In either case, `docker-reuse` updates all references to the image in a
   user-provided template file(s) to contain this exact image tag.

## Usage as a command line tool

`docker-reuse [OPTIONS] PATH IMAGE [ARG...]`

Positional arguments are:

- `PATH`

  Docker build context directory

- `IMAGE`

  Name of the image to find or build

- `[ARG...]`

  Optional build arguments (Format: `NAME[=value]`). If the value is not
  provided, it is taken from the environment variable having the same name as
  the build argument.

Options:

- `-f, --dockerfile=FILE`

  Pathname of the Dockerfile (Default is `PATH/Dockerfile`)

- `-p, --placeholder=PLACEHOLDER`

  Placeholder for the new image name in the file specified by `--template` or
  `--update-in-place` (by default, the image name itself, including the tag).

- `--template FILE`

  Template file to use for the next `--write-to` operation.

- `--write-to FILE`

  File to overwrite with the template file's content where the placeholder is
  replaced with the new image tag. Requires `--template` to be specified
  earlier in the command line.

- `-u, --update-in-place=FILE`

  File to read and update in place with the new image tag (equivalent to
  `--template` and `--write-to` pointing to the same file). Can be specified
  multiple times.

- `-q, --quiet`

  Suppress build output

- `-t, --tag=TAG`

  Additional tag to use for the image (by default, only the 160-bit
  fingerprint computed from the image sources is used). Can be specified
  multiple times.

- `-m, --mode=MODE`

  Fingerprinting mode &mdash; one of the following:

  - `commit` &mdash; use the commit hash as the fingerprint
  - `sha1` &mdash; compute the SHA1 hash of the source files and use the
    combined hash as the fingerprint
  - `auto` &mdash; use the commit hash if available, otherwise fall back to
    `sha1`

- `--platform PLATFORM`

  Target platform for the image (e.g., `linux/amd64`).

- `--check-local-cache`

  If the target image already exists in the local cache, assume it also exists
  in the registry and skip pushing it to the registry. This is an optimization
  shortcut for faster local builds and should be used with caution.

### Example

    docker-reuse \
        -f ./docker/myapp/Dockerfile \
        --update-in-place ./kubernetes/myapp/deployment.yaml \
        -t v1.0.0 \
        -m sha1 \
        ./src/myapp \
        mydockerhubid/myapp

## Usage as a Google Cloud Build builder

When used as a [community Cloud Build
builder](https://github.com/GoogleCloudPlatform/cloud-builders-community/tree/master/docker-reuse),
`docker-reuse` replaces the `docker` builder steps as well as the `images`
field in `cloudbuild.yaml`.

### Cloud Build Example

Here's an example of a trivial but complete `cloudbuild.yaml`:

    steps:
      - id: build-and-push
        name: gcr.io/$PROJECT_ID/docker-reuse
        args: [
            "-f",
            "docker/hello-world/Dockerfile",
            "--update-in-place",
            "kubernetes/hello-world/deployment.yaml", # the file to update
            "-p",
            "IMAGE_PLACEHOLDER", # the string to replace in deployment.yaml
            ".",
            "gcr.io/$PROJECT_ID/hello-world", # the image to build
            "GREETING=Hello, World!", # build-arg value is provided
            "PORT", # build-arg value is taken from the environment
          ]
        env:
          - "PORT=8080"
        timeout: 900s

      - id: deploy
        waitFor: ["build-and-push"]
        name: gcr.io/cloud-builders/kubectl
        args: ["apply", "-k", "kubernetes/hello-world"]
        env:
          - "CLOUDSDK_COMPUTE_ZONE=${_CLUSTER_ZONE}"
          - "CLOUDSDK_CONTAINER_CLUSTER=${_CLUSTER_NAME}"

    substitutions:
      _CLUSTER_ZONE: us-east4-b
      _CLUSTER_NAME: cluster-1

Additional information and working examples can be found on the [community builder
page](https://github.com/GoogleCloudPlatform/cloud-builders-community/tree/master/docker-reuse).
