# syntax=docker/dockerfile:1
FROM scratch
WORKDIR /app
COPY --chown=65532:65532 dist/portal /portal
USER 65532:65532
ENTRYPOINT ["/portal"]
