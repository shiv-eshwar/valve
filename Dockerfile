FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /valved ./cmd/valved

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /valved /usr/local/bin/valved
EXPOSE 8080 9090
ENTRYPOINT ["valved"]
