# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy the Go module files first for better caching
COPY lib/go/go.mod lib/go/go.sum ./lib/go/
WORKDIR /app/lib/go
RUN go mod download

# Copy the rest of the Go source code
COPY lib/go .

# Build the DDoS example to show it off
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /radixip-ddos-demo ./examples/ddos

# Run stage
FROM alpine:latest

WORKDIR /root/
COPY --from=builder /radixip-ddos-demo .

# We can run the demo immediately
CMD ["./radixip-ddos-demo"]
