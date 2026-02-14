FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o devmemory ./cmd/devmemory

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /build/devmemory /usr/local/bin/devmemory
COPY --from=builder /build/migrations /migrations

EXPOSE 8090
ENTRYPOINT ["devmemory"]
CMD ["--migrate"]
