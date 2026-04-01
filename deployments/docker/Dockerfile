FROM golang:1.23-alpine AS builder

WORKDIR /app

RUN apk update && apk add --no-cache git ca-certificates tzdata

# install dependencies
COPY go.mod go.sum ./
RUN go mod download

# copy source code
COPY . .

# compile a static binary by disabling CGO 
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o aetheris cmd/aetheris/main.go


FROM gcr.io/distroless/static-debian12

# retrieve the certificates and time zone from the builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

WORKDIR /

# download the compiled binary and the sample config folder
COPY --from=builder /app/aetheris /aetheris
COPY --from=builder /app/configs /configs

# open the proxy and admin ports to the outside
EXPOSE 8080 9090

ENTRYPOINT ["/aetheris"]