# Build stage
FROM golang:1.24 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /kube-board ./cmd/kube-board

# Runtime stage
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /kube-board /kube-board

USER nonroot:nonroot
ENTRYPOINT ["/kube-board"]
