# Build
FROM golang:1.14.6 AS build-env
WORKDIR /app

ARG PROXY=

COPY . /app

RUN ( if [ ! -z "$PROXY" ]; then export HTTP_PROXY="$PROXY"; export HTTPS_PROXY="$PROXY"; fi ) && \
    go build -o webhook

# Copy built application to actual image
FROM golang:1.14.6

ARG KUBECONFIG

COPY --from=build-env /app/webhook /app

CMD [ "/app/webhook", "-logtostderr", "-v", "10" ]
