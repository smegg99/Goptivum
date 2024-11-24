FROM golang:1.23.3 AS builder

RUN apt-get update && apt-get install -y \
    build-essential \
    git \
    make \
    nodejs \
    npm \
    ca-certificates

WORKDIR /app

COPY . .

RUN make

FROM debian:bookworm-slim

# Install CA certificates for TLS so we can make requests to external services (e.g. zsem.edu.pl)
RUN apt-get update && apt-get install -y ca-certificates

WORKDIR /app

COPY --from=builder /app/build/Goptivum/dist /app/dist
COPY --from=builder /app/build/Goptivum/config.json /app/config.json
COPY --from=builder /app/build/Goptivum/.env /app/.env
COPY --from=builder /app/build/Goptivum/Goptivum /app/Goptivum

RUN chmod +x /app/Goptivum

EXPOSE 3001

CMD ["./Goptivum"]
