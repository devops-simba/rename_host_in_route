# Build
FROM golang:alpine3.12 AS build-env
WORKDIR /app

ARG PROXY=

COPY . /app

RUN ( if [ ! -z "$PROXY" ]; then export HTTP_PROXY="$PROXY"; export HTTPS_PROXY="$PROXY"; fi ) && \
    go build -o webhook

# Copy built application to actual image
FROM alpine:3.12

COPY --from=build-env /app/webhook /app

CMD [ "/app/webhook", "-logtostderr", "-v", "10" ]
