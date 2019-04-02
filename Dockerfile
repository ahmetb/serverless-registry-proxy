FROM golang:1.12-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src/app
COPY . .
RUN go build -o /app

FROM alpine
RUN apk add --no-cache ca-certificates
COPY --from=build /app /app
ENTRYPOINT [ "/app" ]
