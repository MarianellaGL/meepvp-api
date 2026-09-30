FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -trimpath -o /server ./cmd/server && GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -trimpath -o /cli ./cmd/cli

FROM debian:bookworm-slim AS codex-install
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl tar gzip && rm -rf /var/lib/apt/lists/*
ENV CODEX_HOME=/opt/codex-home CODEX_INSTALL_DIR=/opt/codex-bin CODEX_NON_INTERACTIVE=1
RUN curl -fsSL https://chatgpt.com/codex/install.sh -o /tmp/install-codex.sh && sh /tmp/install-codex.sh --release 0.159.0 && /opt/codex-bin/codex --version

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates poppler-utils tesseract-ocr tesseract-ocr-eng tesseract-ocr-spa && rm -rf /var/lib/apt/lists/*
RUN useradd --system --uid 65532 --no-create-home app
COPY --from=codex-install /opt/codex-home/packages/standalone /opt/codex-home/packages/standalone
COPY --from=codex-install /opt/codex-bin /opt/codex-bin
ENV PATH=/opt/codex-bin:$PATH
ENV AI_PROVIDER=codex-cli
RUN codex --version
USER 65532:65532
WORKDIR /
COPY --from=build /server /server
COPY --from=build /cli /cli
EXPOSE 8080
ENTRYPOINT ["/server"]
