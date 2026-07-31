FROM golang:1.22-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git build-base

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o friday ./cmd/friday/

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/friday ./friday
COPY --from=builder /app/go.mod ./go.mod
COPY --from=builder /app/go.sum ./go.sum

EXPOSE 8000

ENTRYPOINT ["./friday"]