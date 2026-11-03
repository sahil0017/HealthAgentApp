FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/cortex-api ./cmd/api && CGO_ENABLED=0 go build -o /out/cortex-worker ./cmd/worker

# The worker needs Tesseract (image OCR) and poppler (scanned PDF pages), so it runs on Alpine instead of distroless.
FROM alpine:3.22 AS worker
ARG TESSERACT_PACKS="tesseract-ocr-data-eng"
RUN apk add --no-cache tesseract-ocr poppler-utils $TESSERACT_PACKS \
    && adduser -D -u 10001 cortex
COPY --from=build /out/cortex-worker /cortex-worker
USER cortex
ENTRYPOINT ["/cortex-worker"]

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cortex-api /cortex-api
COPY --from=build /out/cortex-worker /cortex-worker
EXPOSE 8080
ENTRYPOINT ["/cortex-api"]
