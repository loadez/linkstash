FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/web ./cmd/web

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/web /usr/local/bin/web
EXPOSE 8082
ENTRYPOINT ["/usr/local/bin/web"]
