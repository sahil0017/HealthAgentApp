FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/cortex-api ./cmd/api && CGO_ENABLED=0 go build -o /out/cortex-worker ./cmd/worker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cortex-api /cortex-api
COPY --from=build /out/cortex-worker /cortex-worker
EXPOSE 8080
ENTRYPOINT ["/cortex-api"]
