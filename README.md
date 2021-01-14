# docker-reuse

This is a custom builder for Google Cloud Build that can be used in place of
the `docker` builder. If the sources that go into the container image have not
changed since the image was built last time, `docker-reuse` will find that image
in the repository.
