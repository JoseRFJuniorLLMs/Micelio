FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /micelio ./cmd/micelio/

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /micelio /usr/local/bin/micelio
EXPOSE 9000
ENTRYPOINT ["micelio"]
CMD ["agent"]
