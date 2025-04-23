# go-fluentd-logger-poc

Demo repository for sending Go application logs to Fluentd via Fluent Bit using fluent-logger-golang client.

```bash
# Build and start services
docker compose build
docker compose up
```

```bash
# Verify log pipeline
docker compose logs fluentd | grep "System metrics collected"
```
