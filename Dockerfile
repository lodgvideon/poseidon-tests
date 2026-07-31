# One image carrying both binaries. The driver and the target are always
# built from the same commit, which keeps the payload generator and the
# handler in lockstep — they share internal/payload, and a mismatch would
# silently change the work being measured.
FROM golang:1.26 AS build

WORKDIR /src

# Copy manifests first so dependency download is cached independently of
# source edits — this image gets rebuilt on every code change during a
# tuning session.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 for a static binary that runs on the distroless base.
# Symbols are kept (no -s -w) because the driver serves pprof, and stripped
# binaries produce useless profiles — allocation attribution is the whole
# point of the exercise.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/target  ./cmd/target && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/driver  ./cmd/driver

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/target /target
COPY --from=build /out/driver /driver

USER nonroot:nonroot
ENTRYPOINT ["/target"]
