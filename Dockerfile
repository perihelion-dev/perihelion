# Perihelion node — built from source, run with no ambient privileges.
#
# The result is a single static binary on an empty filesystem: no shell, no
# package manager, no libraries, nothing an attacker who found a flaw in the
# node could pivot to. Suitable for running a node beside unrelated services
# on the same host.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Static, reproducible-ish build: no cgo, trimmed paths, no symbol table.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /perihelion ./cmd/perihelion

FROM scratch
COPY --from=build /perihelion /perihelion
# 65532:65532 is the conventional "nonroot" uid; the image ships no /etc/passwd
# because nothing in it needs to resolve a user name.
USER 65532:65532
VOLUME ["/data"]
EXPOSE 16180
ENTRYPOINT ["/perihelion"]
CMD ["node", "--datadir", "/data", "--rpc", "off"]
