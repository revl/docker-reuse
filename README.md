## Overview

`docker-reuse` is a tool for building and publishing Docker images. It has two
related and complementary purposes:

1.  Make the image content-addressable by tagging it with a fingerprint
    computed from the sources referenced in the Dockerfile.

    As a result, the image tag never changes unless the sources have changed,
    and so Kubernetes (or another orchestration system) won't have to restart
    the containers that use that image.

2.  Save time and resources by bypassing the lengthy build and push operations
    if not a single source has changed.

    This performance improvement is less significant for the local Docker
    builds, which are relatively fast if the sources did not change. However,
    for the environments where Docker layer caching is not available (like
    Google Cloud Build), knowing when it is safe to skip the entire build can
    be an enormous time-saver.

Here's how `docker-reuse` works:

1.  It computes a 160-bit fingerprint from the Dockerfile sources.
2.  It attempts to find a previously built image in the registry using the
    fingerprint as a tag.
3.  If no such image exists, the tool builds it and pushes it to the registry.
4.  In either case, `docker-reuse` updates all references to the image in a
    user-provided file to contain this exact image tag.

## Usage

`docker-reuse [OPTIONS] PATH IMAGE FILE [ARG...]`

Positional arguments are:

*   `PATH`

    Docker build context directory

*   `IMAGE`

    Name of the image to find or build

*   `FILE`

    File to update with the new image tag

*   `[ARG...]`

    Optional build arguments (Format: `NAME[=value]`)

Options:

*   `-f Dockerfile`

    Pathname of the Dockerfile (Default is `PATH/Dockerfile`)

*   `-p string`

    Placeholder for the image name in `FILE` (by default, the image name
    itself).

*   `-q`

    Suppress build output

## Example

    docker-reuse \
        -f ./docker/myapp/Dockerfile \
        ./src/myapp \
        mydockerhubid/myapp \
        ./kubernetes/myapp/deployment.yaml
