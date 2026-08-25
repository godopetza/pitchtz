FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/pitchtz .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S pitchtz \
    && adduser -S -G pitchtz -u 10001 pitchtz
COPY --from=build /out/pitchtz /usr/local/bin/pitchtz

USER pitchtz
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/pitchtz"]
