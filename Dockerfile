FROM golang:1.15.5-alpine3.12 AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src/app
COPY . .
RUN go build -o /app

FROM alpine
RUN apk add --no-cache ca-certificates
COPY --from=build /app /app

# uncomment the following two lines if you're exposing a private GCR registry
# COPY key.json /app/key.json
# ENV GOOGLE_APPLICATION_CREDENTIALS /app/key.json

ENTRYPOINT [ "/app" ]
