FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o homebutler .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/homebutler /usr/local/bin/homebutler
ENTRYPOINT ["homebutler"]
CMD ["mcp"]
