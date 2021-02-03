FROM golang:1.15 AS build

WORKDIR /go/src/docker-reuse

COPY . .

RUN go build -v

FROM gcr.io/cloud-builders/docker:19.03.8@sha256:5df6b8d9eac23f93719446cc452ce31a35f5936991cf45b0e85958cb254a44e7

COPY --from=build /go/src/docker-reuse/docker-reuse /usr/local/bin/

ENV DOCKER_CLI_EXPERIMENTAL=enabled

ENTRYPOINT ["/usr/local/bin/docker-reuse"]
