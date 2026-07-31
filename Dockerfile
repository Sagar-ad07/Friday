FROM golang:1.22-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git build-base

COPY go.mod go.sum ./
RUN go mod download

COPY go/friday ./go/friday
COPY go/trading ./go/trading
COPY go/internal ./go/internal
COPY go/pkg ./go/pkg
COPY go/safety ./go/safety
COPY go/pipeline ./go/pipeline
COPY go/config ./go/config

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o friday ./go/cmd/friday/

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/friday ./friday
COPY --from=builder /app/go/friday/go.mod ./go/friday/go.mod
COPY --from=builder /app/go/friday/go.sum ./go/friday/go.sum

EXPOSE 8000

ENTRYPOINT ["./friday"]