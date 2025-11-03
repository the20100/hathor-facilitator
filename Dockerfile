# Dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/facilitator ./cmd/facilitator

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/facilitator /usr/local/bin/facilitator
ENV PORT=8443
EXPOSE 8443
ENTRYPOINT ["/usr/local/bin/facilitator"]