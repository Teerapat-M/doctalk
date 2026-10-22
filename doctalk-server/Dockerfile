FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /server .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
    && mkdir -p /data/uploads \
    && chown nobody:nobody /data/uploads
WORKDIR /app
COPY --from=build /server /server
ENV PORT=8081
ENV STORAGE_DIR=/data/uploads
EXPOSE 8081
USER nobody
ENTRYPOINT ["/server"]
