# syntax=docker/dockerfile:1.7

FROM golang:1.25.10-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/yavchn ./src

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/yavchn /yavchn
ENV YAVCHN_DB_PATH=/home/nonroot/yavchn.db
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/yavchn"]
