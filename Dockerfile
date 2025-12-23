FROM golang:1.21 AS build
COPY . /workdir
WORKDIR /workdir
RUN CGO_ENABLED=0 go build -v

FROM build AS test
WORKDIR /workdir
RUN go test -v ./...

FROM gcr.io/cloud-builders/docker
COPY --from=build /workdir/docker-reuse /usr/local/bin/
ENV DOCKER_CLI_EXPERIMENTAL=enabled
ENTRYPOINT ["/usr/local/bin/docker-reuse"]
