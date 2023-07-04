# This Dockerfile is used to build the image available on DockerHub
FROM golang:1.19 as build

# Add everything
ADD . /usr/src/bridge-operator

RUN cd /usr/src/bridge-operator && \
    go build ./cmd/bridge-operator-daemon

FROM fedora:38
LABEL org.opencontainers.image.source https://github.com/s1061123/bridge-operator
COPY --from=build /usr/src/bridge-operator/bridge-operator-daemon /usr/bin
WORKDIR /usr/bin

ENTRYPOINT ["bridge-operator-daemon"]
