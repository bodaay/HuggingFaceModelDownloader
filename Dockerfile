# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache make

COPY . ./
COPY makefile ./Makefile

RUN make build

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/output/hfdownloader_linux_amd64_2.0.0 ./hfdownloader

ENTRYPOINT ["./hfdownloader"]
