FROM golang:1.19.5-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src/app
COPY . .
RUN go build -o /app

FROM alpine
RUN apk add --no-cache ca-certificates
COPY --from=build /app /app

# uncomment the following two lines if you're exposing a private GCR registry
# COPY key.json /key.json
# ENV GOOGLE_APPLICATION_CREDENTIALS /key.json

ENTRYPOINT [ "/app" ]
