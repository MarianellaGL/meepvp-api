FROM golang:1.27-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /server ./cmd/server && CGO_ENABLED=0 go build -trimpath -o /cli ./cmd/cli

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates poppler-utils tesseract-ocr tesseract-ocr-eng tesseract-ocr-spa && rm -rf /var/lib/apt/lists/*
RUN useradd --system --uid 65532 --no-create-home app
USER 65532:65532
WORKDIR /
COPY --from=build /server /server
COPY --from=build /cli /cli
EXPOSE 8080
ENTRYPOINT ["/server"]
