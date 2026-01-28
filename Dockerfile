FROM golang:alpine

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

WORKDIR /go/src/app
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download -x
ENV USER=go \
    UID=1000 \
    GID=1000 \
    CGO_ENABLED=0 \
    GOCACHE=/root/.cache/go-build

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=cache,target="/root/.cache/go-build" \
    go mod tidy && \
    go build -ldflags="-s -w \
    -X '${MODULE}/cmd.version=${VERSION}' \
    -X '${MODULE}/cmd.commit=${COMMIT}' \
    -X '${MODULE}/cmd.date=${DATE}'" \
    -o dead-mans-switch && \
    addgroup --gid "$GID" "$USER" && \
    adduser \
    --disabled-password \
    --gecos "" \
    --home "$(pwd)" \
    --ingroup "$USER" \
    --no-create-home \
    --uid "$UID" \
    "$USER" && \
    chown "$UID":"$GID" /go/src/app/dead-mans-switch && \
    mkdir /cache && \
    chown -R "$UID":"$GID" /cache

FROM scratch
COPY --from=0 /etc/passwd /etc/passwd
COPY --from=0 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=0 --chown=1000:1000 /cache /cache
COPY --from=0 /go/src/app/dead-mans-switch /
VOLUME /cache
# Persist certs from certmagic
ENV HOME=/cache
# Persist database/secret key
VOLUME /data
USER 1000
EXPOSE 8080/tcp
EXPOSE 80/tcp
EXPOSE 443/tcp
ENTRYPOINT ["/dead-mans-switch"]
CMD ["server", "--storage-dir", "/data"]
